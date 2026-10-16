package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zerodenet/zboard/backend/internal/readtiming"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

type runtimeQueue = jobs.RuntimeQueueSummary

type runtimeStatusIssue struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

func (h *handlers) AdminRuntimeRegisteredJobsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	OK(w, h.services.Jobs.RegisteredJobs())
}

func (h *handlers) AdminRuntimeJobsHandler(w http.ResponseWriter, r *http.Request) {
	claims, authErr := h.requireAdmin(w, r)
	if authErr != nil {
		return
	}
	view := r.URL.Query().Get("view")
	if view != "" && view != "execution" && view != "queues" && view != "runtime" && view != "plugins" {
		BadRequest(w, "未知运行状态视图")
		return
	}
	include := func(part string) bool { return view == "" || view == part }
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
	if include("plugins") && h.pluginManager != nil {
		status := h.pluginManager.Status()
		host = &status
	}
	var wg sync.WaitGroup
	if include("queues") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startedAt := time.Now()
			defer func() { queueDuration = time.Since(startedAt) }()
			queues, queueErr = h.services.RuntimeQueues().Summaries(ctx, now)
		}()
	}
	if include("runtime") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startedAt := time.Now()
			defer func() { diagnosticsDuration = time.Since(startedAt) }()
			diagnostics, diagnosticsErr = h.runtimeDiagnostics()
		}()
	}
	if include("execution") {
		wg.Add(1)
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
	}
	if include("queues") {
		wg.Add(1)
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
	}
	if include("plugins") && h.pluginManager != nil {
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
	if include("queues") && registrationErr == nil {
		queues = append(queues, registration)
	}
	data := map[string]any{
		"as_of": now, "started_at": zboardProcessStartedAt, "observation_scope": "deployment",
		"jobs": executionJobs, "registered_jobs": h.services.Jobs.RegisteredJobs(), "queues": queues, "runtime": map[string]any{}, "issues": issues,
		"read_duration_ms": map[string]int64{"queues": queueDuration.Milliseconds(), "runtime": diagnosticsDuration.Milliseconds(), "execution": executionDuration.Milliseconds(), "registration_messages": registrationDuration.Milliseconds(), "plugin_tasks": pluginTasksDuration.Milliseconds()},
		"plugin_host":      host, "plugin_tasks": pluginTasks,
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
	if view != "" {
		fields := map[string][]string{
			"execution": {"jobs", "registered_jobs", "execution_queue", "execution_concurrency", "external_execution_concurrency", "admin_task_concurrency", "admin_item_concurrency"},
			"queues":    {"queues"}, "runtime": {"runtime"},
			"plugins": {"plugin_host", "plugin_tasks"},
		}
		focused := map[string]any{"as_of": now, "started_at": zboardProcessStartedAt, "observation_scope": "deployment", "issues": issues, "view": view}
		for _, key := range fields[view] {
			if value, ok := data[key]; ok {
				focused[key] = value
			}
		}
		durations := map[string]int64{}
		for key, ms := range data["read_duration_ms"].(map[string]int64) {
			if key == view || (view == "queues" && key == "registration_messages") || (view == "plugins" && key == "plugin_tasks") {
				durations[key] = ms
			}
		}
		focused["read_duration_ms"] = durations
		data = focused
	}
	timing := make([]string, 0, 6)
	for _, part := range []struct {
		view, name string
		duration   time.Duration
	}{
		{"queues", "runtime_queues", queueDuration}, {"runtime", "diagnostics", diagnosticsDuration},
		{"execution", "execution", executionDuration}, {"queues", "registration", registrationDuration},
		{"plugins", "plugin_tasks", pluginTasksDuration},
	} {
		if include(part.view) {
			readtiming.Record(ctx, part.name, part.duration)
			timing = append(timing, fmt.Sprintf("%s;dur=%d", part.name, part.duration.Milliseconds()))
		}
	}
	timing = append(timing, fmt.Sprintf("total;dur=%d", time.Since(requestStartedAt).Milliseconds()))
	w.Header().Set("Server-Timing", strings.Join(timing, ", "))
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

func (h *handlers) AdminRuntimePublicationRetryHandler(w http.ResponseWriter, r *http.Request) {
	claims, authErr := h.requireAdmin(w, r)
	if authErr != nil {
		return
	}
	id, err := strconv.ParseUint(pathvar.Vars(r)["id"], 10, 32)
	if err != nil || id == 0 {
		BadRequest(w, "节点编号无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.services.RuntimeQueues().RetryPublication(ctx, uint(id), claims.UserID, claims.Email, time.Now().UTC()); err != nil {
		if errors.Is(err, jobs.ErrRuntimeQueueConflict) {
			writeJSON(w, http.StatusConflict, "发布请求已被领取、完成或正在等待领取，请刷新", nil)
			return
		}
		ServiceUnavailable(w, "重试请求保存失败")
		return
	}
	OK(w, map[string]any{"node_id": id, "status": "queued"})
}
