package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

type runtimeQueue = jobs.RuntimeQueueSummary

type runtimeStatusIssue struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

func (h *handlers) AdminRuntimeJobsHandler(w http.ResponseWriter, r *http.Request) {
	claims, authErr := h.requireAdmin(w, r)
	if authErr != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	requestStartedAt := time.Now()
	now := time.Now().UTC()
	queues := make([]runtimeQueue, 0, 3)
	pluginTasks := make([]plugins.TaskView, 0)
	var diagnostics runtimeDiagnostics
	var executionErr, queueErr, diagnosticsErr, pluginTasksErr, registrationErr error
	var executionQueue any
	var executionJobs any = []any{}
	var executionConcurrency, externalExecutionConcurrency int
	var registration runtimeQueue
	var queueDuration, diagnosticsDuration, executionDuration, registrationDuration, pluginTasksDuration time.Duration
	var host *plugins.HostStatus
	if h.pluginManager != nil {
		status := h.pluginManager.Status()
		host = &status
	}
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		startedAt := time.Now()
		defer func() { queueDuration = time.Since(startedAt) }()
		queues, queueErr = h.services.RuntimeQueues().Summaries(ctx, now)
	}()
	go func() {
		defer wg.Done()
		startedAt := time.Now()
		defer func() { diagnosticsDuration = time.Since(startedAt) }()
		diagnostics, diagnosticsErr = h.runtimeDiagnostics()
	}()
	go func() {
		defer wg.Done()
		startedAt := time.Now()
		defer func() { executionDuration = time.Since(startedAt) }()
		execution, err := h.services.RuntimeExecutionStatus(ctx)
		executionErr = err
		if err == nil {
			executionJobs = execution.Jobs
			executionQueue = execution.Queue
			executionConcurrency = execution.Capacity
			externalExecutionConcurrency = execution.ExternalCapacity
		}
	}()
	go func() {
		defer wg.Done()
		startedAt := time.Now()
		defer func() { registrationDuration = time.Since(startedAt) }()
		events, err := h.services.RegistrationEventStatus().Summary(ctx, claims.UserID)
		registrationErr = err
		if err == nil {
			registration = runtimeQueue{ID: "registration_messages", Name: "注册消息", Pending: events.Pending, OldestAt: events.OldestAt}
		}
	}()
	if h.pluginManager != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startedAt := time.Now()
			defer func() { pluginTasksDuration = time.Since(startedAt) }()
			pluginTasks, pluginTasksErr = h.pluginManager.TaskSnapshots(ctx)
		}()
	}
	wg.Wait()

	issues := make([]runtimeStatusIssue, 0, 5)
	addIssue := func(section, message string, err error) {
		if err == nil {
			return
		}
		log.Printf("runtime status partial snapshot: section=%s error=%v", section, err)
		issues = append(issues, runtimeStatusIssue{Section: section, Message: message})
	}
	addIssue("queues", "业务队列状态读取失败", queueErr)
	addIssue("runtime", "事件队列与进程诊断读取失败", diagnosticsErr)
	addIssue("execution", "任务执行状态读取失败", executionErr)
	addIssue("plugin_tasks", "插件任务状态读取失败", pluginTasksErr)
	addIssue("registration_messages", "注册消息状态读取失败", registrationErr)
	if registrationErr == nil {
		queues = append(queues, registration)
	}
	data := map[string]any{
		"as_of": now, "started_at": zboardProcessStartedAt, "observation_scope": "deployment",
		"jobs": executionJobs, "queues": queues, "runtime": map[string]any{}, "issues": issues,
		"plugin_host": host, "plugin_tasks": pluginTasks,
		"admin_task_concurrency": adminTaskWorkers, "admin_item_concurrency": adminTaskWorkers * operationTaskWorkers,
	}
	if diagnosticsErr == nil {
		data["runtime"] = diagnostics
	}
	if executionErr == nil {
		data["execution_queue"] = executionQueue
		data["execution_concurrency"] = executionConcurrency
		data["external_execution_concurrency"] = externalExecutionConcurrency
		data["plugin_task_concurrency"] = executionConcurrency
	}
	w.Header().Set("Server-Timing", fmt.Sprintf("runtime_queues;dur=%d, diagnostics;dur=%d, execution;dur=%d, registration;dur=%d, plugin_tasks;dur=%d, total;dur=%d",
		queueDuration.Milliseconds(), diagnosticsDuration.Milliseconds(), executionDuration.Milliseconds(), registrationDuration.Milliseconds(), pluginTasksDuration.Milliseconds(), time.Since(requestStartedAt).Milliseconds()))
	OK(w, data)
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
