package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const nodeLoadCommand = `LC_ALL=C; export LC_ALL
printf 'cpu_cores '; getconf _NPROCESSORS_ONLN
printf 'load_average '; cat /proc/loadavg
grep -E '^(MemTotal|MemAvailable):' /proc/meminfo
printf 'root_filesystem '; df -Pk / | tail -n 1
printf 'uptime '; cat /proc/uptime`

type nodeLoadSnapshot struct {
	NodeID               uint      `json:"node_id"`
	SampledAt            time.Time `json:"sampled_at"`
	CPUCoreCount         uint64    `json:"cpu_core_count"`
	LoadAverage1         float64   `json:"load_average_1"`
	LoadAverage5         float64   `json:"load_average_5"`
	LoadAverage15        float64   `json:"load_average_15"`
	MemoryTotalBytes     uint64    `json:"memory_total_bytes"`
	MemoryAvailableBytes uint64    `json:"memory_available_bytes"`
	RootTotalBytes       uint64    `json:"root_total_bytes"`
	RootAvailableBytes   uint64    `json:"root_available_bytes"`
	UptimeSeconds        uint64    `json:"uptime_seconds"`
	LatencyMS            int64     `json:"latency_ms"`
}

func (h *handlers) NodeLoadHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if strings.TrimSpace(node.SSHHost) == "" || strings.TrimSpace(node.SSHUser) == "" || node.SSHVerifiedAt == nil || strings.TrimSpace(node.SSHHostKeyFingerprint) == "" {
		BadRequest(w, "节点尚未完成 SSH 验证，无法读取主机资源。")
		return
	}
	output, latency, err := h.execSSHCommand(node, nodeLoadCommand)
	if err != nil {
		BadRequest(w, "读取节点负载失败："+err.Error())
		return
	}
	snapshot, err := parseNodeLoadSnapshot(output)
	if err != nil {
		BadRequest(w, "节点返回了无法识别的负载信息："+err.Error())
		return
	}
	snapshot.NodeID = node.ID
	snapshot.SampledAt = time.Now().UTC()
	snapshot.LatencyMS = latency.Milliseconds()
	OK(w, snapshot)
}

func parseNodeLoadSnapshot(output string) (nodeLoadSnapshot, error) {
	var snapshot nodeLoadSnapshot
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		fields := strings.Fields(strings.TrimSpace(rawLine))
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "cpu_cores":
			value, err := parsePositiveUint(fields[1])
			if err != nil {
				return snapshot, fmt.Errorf("cpu cores: %w", err)
			}
			snapshot.CPUCoreCount, seen[fields[0]] = value, true
		case "load_average":
			if len(fields) < 4 {
				return snapshot, errors.New("load average is incomplete")
			}
			values := []*float64{&snapshot.LoadAverage1, &snapshot.LoadAverage5, &snapshot.LoadAverage15}
			for index := range values {
				value, err := strconv.ParseFloat(fields[index+1], 64)
				if err != nil || value < 0 {
					return snapshot, errors.New("load average is invalid")
				}
				*values[index] = value
			}
			seen[fields[0]] = true
		case "MemTotal:", "MemAvailable:":
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil || value > ^uint64(0)/1024 {
				return snapshot, errors.New("memory value is invalid")
			}
			if fields[0] == "MemTotal:" {
				snapshot.MemoryTotalBytes = value * 1024
			} else {
				snapshot.MemoryAvailableBytes = value * 1024
			}
			seen[fields[0]] = true
		case "root_filesystem":
			if len(fields) < 6 {
				return snapshot, errors.New("root filesystem value is incomplete")
			}
			total, totalErr := strconv.ParseUint(fields[2], 10, 64)
			available, availableErr := strconv.ParseUint(fields[4], 10, 64)
			if totalErr != nil || availableErr != nil || total > ^uint64(0)/1024 || available > ^uint64(0)/1024 {
				return snapshot, errors.New("root filesystem value is invalid")
			}
			snapshot.RootTotalBytes, snapshot.RootAvailableBytes = total*1024, available*1024
			seen[fields[0]] = true
		case "uptime":
			value, err := strconv.ParseFloat(fields[1], 64)
			if err != nil || value < 0 {
				return snapshot, errors.New("uptime is invalid")
			}
			snapshot.UptimeSeconds, seen[fields[0]] = uint64(value), true
		}
	}
	for _, field := range []string{"cpu_cores", "load_average", "MemTotal:", "MemAvailable:", "root_filesystem", "uptime"} {
		if !seen[field] {
			return snapshot, fmt.Errorf("missing %s", field)
		}
	}
	if snapshot.MemoryAvailableBytes > snapshot.MemoryTotalBytes || snapshot.RootAvailableBytes > snapshot.RootTotalBytes {
		return snapshot, errors.New("available capacity exceeds total capacity")
	}
	return snapshot, nil
}

func parsePositiveUint(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, errors.New("value must be a positive integer")
	}
	return parsed, nil
}
