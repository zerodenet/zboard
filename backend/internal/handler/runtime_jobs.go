package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"net/http"
	"time"
)

type runtimeQueue = jobs.RuntimeQueueSummary

func (h *handlers) AdminRuntimeJobsHandler(w http.ResponseWriter, r *http.Request) {
	claims, authErr := h.requireAdmin(w, r)
	if authErr != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	queues, err := h.services.RuntimeQueues().Summaries(ctx, now)
	if err != nil {
		ServiceUnavailable(w, "运行队列读取失败，请稍后重试")
		return
	}
	diagnostics, err := h.runtimeDiagnostics()
	if err != nil {
		ServiceUnavailable(w, "运行状态读取失败")
		return
	}
	var host *plugins.HostStatus
	var pluginTasks []plugins.TaskView
	if h.pluginManager != nil {
		status := h.pluginManager.Status()
		host = &status
		pluginTasks, err = h.pluginManager.TaskSnapshots(ctx)
		if err != nil {
			ServiceUnavailable(w, "插件任务读取失败，请稍后重试")
			return
		}
	}
	execution, err := h.services.RuntimeExecutionStatus(ctx)
	if err != nil {
		ServiceUnavailable(w, "持久化任务读取失败，请稍后重试")
		return
	}
	events, err := h.services.RegistrationEventStatus().Pending(ctx, claims.UserID, 1, 0)
	if err != nil {
		ServiceUnavailable(w, "注册消息状态读取失败")
		return
	}
	registrations := runtimeQueue{ID: "registration_messages", Name: "注册消息", Pending: events.Pending, OldestAt: events.OldestAt}
	queues = append(queues, registrations)
	OK(w, map[string]any{"as_of": now, "started_at": zboardProcessStartedAt, "observation_scope": "deployment", "jobs": execution.Jobs, "execution_concurrency": execution.Capacity, "external_execution_concurrency": execution.ExternalCapacity, "execution_queue": execution.Queue, "queues": queues, "runtime": diagnostics, "plugin_host": host, "plugin_tasks": pluginTasks, "plugin_task_concurrency": execution.Capacity, "admin_task_concurrency": adminTaskWorkers, "admin_item_concurrency": adminTaskWorkers * operationTaskWorkers})
}

type runtimeQueueItem = jobs.RuntimeQueueItem

func (h *handlers) AdminRuntimeQueueHandler(w http.ResponseWriter, r *http.Request) {
	claims, authErr := h.requireAdmin(w, r)
	if authErr != nil {
		return
	}
	name := r.URL.Query().Get("name")
	if name != "node_publish" && name != "admin_tasks" && name != "registration_messages" {
		BadRequest(w, "未知队列")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	limit, offset := queryInt(r, "limit", 25, 1, 50), queryInt(r, "offset", 0, 0, 1000000)
	now := time.Now().UTC()
	if name == "registration_messages" {
		out := []runtimeQueueItem{}
		page, err := h.services.RegistrationEventStatus().Pending(ctx, claims.UserID, limit, offset)
		if err != nil {
			ServiceUnavailable(w, "注册消息队列读取失败")
			return
		}
		for _, item := range page.Items {
			out = append(out, runtimeQueueItem{ID: item.AccountID, Kind: "registration_event", State: "pending", CreatedAt: item.OccurredAt})
		}
		OK(w, pagedData(out, page.Pending, offset, limit))
		return
	}
	page, err := h.services.RuntimeQueues().Page(ctx, name, now, limit, offset)
	if err != nil {
		ServiceUnavailable(w, "队列读取失败")
		return
	}
	for i := range page.Items {
		page.Items[i].LastError = sanitizeAuditDetail(truncateTaskError(page.Items[i].LastError))
	}
	OK(w, pagedData(page.Items, page.Total, offset, limit))
}
