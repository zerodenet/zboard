package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func (h *handlers) AdminRuntimeHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	limit, offset := queryInt(r, "limit", 25, 1, 50), queryInt(r, "offset", 0, 0, 1000000)
	rows, total, err := h.services.JobHistory.Page(ctx, limit, offset)
	if err != nil {
		ServiceUnavailable(w, "执行记录读取失败")
		return
	}
	type row struct {
		ID            string     `json:"id"`
		Owner         string     `json:"owner"`
		Handler       string     `json:"handler"`
		State         jobs.State `json:"state"`
		Attempts      int        `json:"attempts"`
		MaxAttempts   int        `json:"max_attempts"`
		NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
		PlannedAt     *time.Time `json:"planned_at,omitempty"`
		CreatedAt     time.Time  `json:"created_at"`
		FinishedAt    *time.Time `json:"finished_at"`
	}
	out := make([]row, 0, len(rows))
	for _, v := range rows {
		var next *time.Time
		if v.State == jobs.RetryWait {
			value := v.NotBefore
			next = &value
		}
		out = append(out, row{ID: v.ID, Owner: v.Owner, Handler: v.Handler, State: v.State, Attempts: v.Attempt, MaxAttempts: v.MaxAttempts, NextAttemptAt: next, PlannedAt: v.PlannedAt, CreatedAt: v.CreatedAt, FinishedAt: v.FinishedAt})
	}
	OK(w, pagedData(out, total, offset, limit))
}

func (h *handlers) AdminRuntimeAttemptsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	limit, offset := queryInt(r, "limit", 25, 1, 50), queryInt(r, "offset", 0, 0, 1000000)
	rows, total, err := h.services.JobHistory.Attempts(ctx, pathvar.Vars(r)["id"], limit, offset)
	if errors.Is(err, jobs.ErrInvalid) {
		BadRequest(w, "无效的执行记录")
		return
	}
	if err != nil {
		ServiceUnavailable(w, "尝试记录读取失败")
		return
	}
	OK(w, pagedData(rows, total, offset, limit))
}

func (h *handlers) AdminRuntimeCancelHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(body.Reason) == "" || utf8.RuneCountInString(body.Reason) > 500 {
		BadRequest(w, "请提供取消原因")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	state, err := h.services.JobCancellations.Cancel(ctx, jobs.Reviewer{AccountID: claims.UserID}, jobs.Cancellation{RunID: pathvar.Vars(r)["id"], Reason: sanitizeAuditDetail(body.Reason)})
	if errors.Is(err, jobs.ErrPermission) {
		writeJSON(w, http.StatusForbidden, "没有取消任务的权限", nil)
		return
	}
	if errors.Is(err, jobs.ErrInvalid) {
		BadRequest(w, "请提供有效的执行记录与取消原因")
		return
	}
	if errors.Is(err, jobs.ErrConflict) {
		writeJSON(w, http.StatusConflict, "执行状态已结束或不可取消，请刷新", nil)
		return
	}
	if err != nil {
		ServiceUnavailable(w, "取消请求保存失败")
		return
	}
	OK(w, map[string]any{"id": pathvar.Vars(r)["id"], "state": state})
}

func (h *handlers) AdminRuntimeResolveHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var body struct {
		Outcome jobs.State `json:"outcome"`
		Reason  string     `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(body.Reason) == "" || utf8.RuneCountInString(body.Reason) > 500 || (body.Outcome != jobs.Succeeded && body.Outcome != jobs.Failed) {
		BadRequest(w, "请提供核验结果与依据")
		return
	}
	id := pathvar.Vars(r)["id"]
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err = h.services.JobReviews.Resolve(ctx, jobs.Reviewer{AccountID: claims.UserID}, jobs.Review{RunID: id, Outcome: body.Outcome, Reason: sanitizeAuditDetail(body.Reason)})
	if errors.Is(err, jobs.ErrOutcomeUnverified) {
		writeJSON(w, http.StatusConflict, "资源操作尚未完成结果协调，请先核验对应操作。", nil)
		return
	}
	if errors.Is(err, jobs.ErrPermission) {
		writeJSON(w, http.StatusForbidden, "没有核验任务的权限", nil)
		return
	}
	if errors.Is(err, jobs.ErrInvalid) {
		BadRequest(w, "请提供核验结果与依据")
		return
	}
	if errors.Is(err, jobs.ErrConflict) {
		writeJSON(w, http.StatusConflict, "执行状态已变化，请刷新", nil)
		return
	}
	if err != nil {
		ServiceUnavailable(w, "核验结果保存失败")
		return
	}
	OK(w, map[string]any{"id": id, "state": body.Outcome})
}
