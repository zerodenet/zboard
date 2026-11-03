//go:build linux

package hostmetrics

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"golang.org/x/sys/unix"
)

type Reader struct {
	readFile                             func(string) ([]byte, error)
	now                                  func() time.Time
	previousAt                           time.Time
	cpuTotal, cpuIdle                    uint64
	processCPU                           float64
	rx, tx                               uint64
	cpuValid, processValid, networkValid bool
}

func New() *Reader           { return &Reader{readFile: os.ReadFile, now: time.Now} }
func number(s string) uint64 { n, _ := strconv.ParseUint(s, 10, 64); return n }
func ptr(v float64) *float64 { return &v }
func (r *Reader) Read() platform.SystemSample {
	now := r.now().UTC()
	elapsed := now.Sub(r.previousAt).Seconds()
	out := platform.SystemSample{At: now, CPUCount: runtime.NumCPU(), Goroutines: runtime.NumGoroutine(), Issues: []string{}}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out.Heap = mem.HeapAlloc
	cpuOK, networkOK, processOK := false, false, false
	if b, e := r.readFile("/proc/stat"); e == nil {
		f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
		if len(f) >= 9 && f[0] == "cpu" {
			var total uint64
			for _, v := range f[1:9] {
				total += number(v)
			}
			idle := number(f[4]) + number(f[5])
			out.CPU = cpuPercent(total, idle, r.cpuTotal, r.cpuIdle, r.cpuValid)
			r.cpuTotal, r.cpuIdle = total, idle
			cpuOK = true
		}
	}
	if !cpuOK {
		out.Issues = append(out.Issues, "主机 CPU 采集不可用")
	}
	r.cpuValid = cpuOK
	if b, e := r.readFile("/proc/loadavg"); e == nil {
		f := strings.Fields(string(b))
		if len(f) > 0 {
			if v, e := strconv.ParseFloat(f[0], 64); e == nil {
				out.Load1 = &v
			}
		}
	}
	if b, e := r.readFile("/proc/meminfo"); e == nil {
		values := map[string]uint64{}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				values[f[0]] = number(f[1]) * 1024
			}
		}
		total := values["MemTotal:"]
		available, ok := values["MemAvailable:"]
		if ok && total > 0 && available <= total {
			out.Memory = &platform.ResourceUsage{Used: total - available, Total: total, Available: available}
		}
	}
	if out.Memory == nil {
		out.Issues = append(out.Issues, "主机内存采集不可用")
	}
	var disk unix.Statfs_t
	if e := unix.Statfs("/", &disk); e == nil && disk.Blocks > 0 && disk.Bsize > 0 {
		out.Disk = &platform.ResourceUsage{Used: (disk.Blocks - disk.Bfree) * uint64(disk.Bsize), Total: disk.Blocks * uint64(disk.Bsize), Available: disk.Bavail * uint64(disk.Bsize)}
	} else {
		out.Issues = append(out.Issues, "文件系统容量采集不可用")
	}
	if b, e := r.readFile("/proc/self/statm"); e == nil {
		f := strings.Fields(string(b))
		if len(f) > 1 {
			rss := number(f[1]) * uint64(os.Getpagesize())
			out.ProcessRSS = &rss
		}
	}
	var usage unix.Rusage
	if e := unix.Getrusage(unix.RUSAGE_SELF, &usage); e == nil {
		cpu := float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
		if r.processValid && elapsed > 0 && cpu >= r.processCPU {
			out.ProcessCPU = ptr((cpu - r.processCPU) / elapsed)
		}
		r.processCPU = cpu
		processOK = true
	}
	if !processOK || out.ProcessRSS == nil {
		out.Issues = append(out.Issues, "ZBoard 进程采集不完整")
	}
	r.processValid = processOK
	if b, e := r.readFile("/proc/net/dev"); e == nil {
		var rx, tx uint64
		for _, line := range strings.Split(string(b), "\n") {
			pair := strings.SplitN(line, ":", 2)
			if len(pair) != 2 || strings.TrimSpace(pair[0]) == "lo" {
				continue
			}
			f := strings.Fields(pair[1])
			if len(f) < 16 {
				continue
			}
			rx += number(f[0])
			tx += number(f[8])
			networkOK = true
		}
		if networkOK {
			out.RX = counterRate(rx, r.rx, elapsed, r.networkValid)
			out.TX = counterRate(tx, r.tx, elapsed, r.networkValid)
		}
		r.rx, r.tx = rx, tx
	}
	if !networkOK {
		out.Issues = append(out.Issues, "实例网络采集不可用")
	}
	r.networkValid = networkOK
	r.previousAt = now
	return out
}
