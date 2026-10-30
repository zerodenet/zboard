package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (h *handlers) NodeRetirementHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	value := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/nodes/"), "/retirement")
	value = strings.TrimSuffix(value, "/deletion")
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil || id == 0 {
		BadRequest(w, "节点编号无效")
		return
	}
	service := h.services.NodeRetirement(h)
	if r.Method == http.MethodGet {
		result, err := service.Repository.Read(r.Context(), uint(id))
		if err != nil {
			resourceRemovalError(w, err)
			return
		}
		OK(w, result)
		return
	}
	var input struct {
		Mode string `json:"mode"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		BadRequest(w, "请提供有效的删除请求")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/deletion") && input.Mode != "delete" {
		BadRequest(w, "删除请求必须使用 delete 方式")
		return
	}
	lock := h.nodePublishLock(uint(id))
	if err := lock.Lock(r.Context()); err != nil {
		BadRequest(w, "节点正在处理其他操作")
		return
	}
	defer lock.Unlock()
	h.backgroundJobs()
	result, err := service.Repository.Start(r.Context(), actor.UserID, uint(id), input.Mode)
	if err != nil {
		var blocked *network.RetirementBlocked
		if errors.As(err, &blocked) {
			writeJSON(w, http.StatusConflict, blocked.Reason, nil)
		} else if errors.Is(err, jobs.ErrInvalid) {
			BadRequest(w, "删除请求的处理方式无效")
		} else {
			resourceRemovalError(w, err)
		}
		return
	}
	h.invalidateZeroEventCredential(uint(id))
	writeJSON(w, http.StatusAccepted, "节点删除任务已提交", result)
}
func (h *handlers) executeNodeRetirement(ctx context.Context, run jobs.Run) error {
	err := h.services.NodeRetirement(h).Execute(ctx, run)
	if err == nil {
		var input struct {
			NodeID uint `json:"node_id"`
		}
		if json.Unmarshal([]byte(run.Payload), &input) == nil {
			h.invalidateZeroEventCredential(input.NodeID)
		}
	}
	return err
}
