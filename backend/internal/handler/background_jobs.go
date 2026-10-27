package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/application"
	capabilityjobs "github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/jobrun"
	"log"
	"strings"
	"sync"
	"time"
)

type scheduledJob struct {
}
type backgroundJobs struct {
	observations *jobrun.Registry
	mu           sync.Mutex
	loops        map[string]*scheduledJob
}

var jobNames = map[string]string{"discount_redemptions": "优惠预留确认与恢复", "traffic_diagnostic_retention": "流量诊断材料保留", "node_observation_material_retention": "节点观察材料保留", application.NodeObservationJobID: application.NodeObservationJobName, application.UsageProjectionJobID: application.UsageProjectionJobName, "automatic_renewal": "余额自动续费", "wallet_reservations": "余额预留到期释放", "subscription_alerts": "订阅告警扫描", "credential_expiry": "订阅到期处理", "traffic_reset": "订阅流量重置", "fair_use": "公平使用评估", "certificate_renewal": "证书续期扫描", "dns_observation": "DNS 公网核验", "history_retention": "历史记录清理", "proxy_pool_sync": "代理池订阅同步", "node_publish": "节点配置发布", "admin_tasks": "运营任务执行", "registration_messages": "注册消息处理", "event_consumer": "流量事件入账"}

func (h *handlers) backgroundJobs() *backgroundJobs {
	h.jobRuntimeOnce.Do(func() {
		h.jobRuntime = &backgroundJobs{observations: jobrun.New(), loops: map[string]*scheduledJob{}}
		h.registerNativeJobs(h.services.Jobs)
	})
	return h.jobRuntime
}
func (h *handlers) observeJob(id, kind string, interval time.Duration, concurrency int) func(error) {
	r := h.backgroundJobs().observations
	r.Register(id, jobNames[id], kind, interval, concurrency)
	finish := r.Begin(id)
	return func(err error) {
		message := ""
		if err != nil && !capabilityjobs.IsContinuation(err) {
			message = sanitizeAuditDetail(truncateTaskError(err.Error()))
		}
		finish(message)
	}
}
func (h *handlers) startScheduledJob(id string, interval time.Duration, run func(context.Context) error) {
	name := jobNames[id]
	if strings.HasPrefix(id, "node_publish_") {
		name = "节点配置发布 · " + strings.TrimPrefix(id, "node_publish_")
	}
	h.startPeriodicJob(application.PeriodicJob{
		ID: id, Name: name, Interval: interval, Timeout: 30 * time.Minute, Immediate: true,
		External:           id == "admin_tasks" || strings.HasPrefix(id, "node_publish_"),
		ReconcileAfterLoss: id == "subscription_alerts" || id == "registration_messages" || id == "event_consumer" || id == "usage_projection" || id == "node_observation" || id == "node_observation_material_retention" || id == "traffic_diagnostic_retention" || id == "admin_tasks" || id == "automatic_renewal" || id == "wallet_reservations" || id == "credential_expiry" || id == "traffic_reset" || id == "history_retention" || strings.HasPrefix(id, "node_publish_"),
		RetryFailures:      id == "subscription_alerts" || id == "registration_messages" || id == "event_consumer" || id == "usage_projection" || id == "node_observation" || id == "node_observation_material_retention" || id == "traffic_diagnostic_retention" || id == "admin_tasks" || id == "automatic_renewal" || id == "wallet_reservations" || id == "credential_expiry" || id == "traffic_reset" || id == "history_retention" || strings.HasPrefix(id, "node_publish_"),
		Ready:              h.jobReadiness(id),
		Run:                run,
	})
}

// startPeriodicJob is only the transport's existing duplicate-registration and
// process-local observation shell. Durable scheduling identity/policy comes
// from the supplied application definition; shutdown/revocation is unchanged.
func (h *handlers) startPeriodicJob(job application.PeriodicJob) {
	bg := h.backgroundJobs()
	bg.mu.Lock()
	defer bg.mu.Unlock()
	if bg.loops[job.ID] != nil {
		return
	}
	bg.observations.Register(job.ID, job.Name, "periodic", job.Interval, 1)
	run := job.Run
	job.Run = func(ctx context.Context) (err error) {
		finish := h.observeJob(job.ID, "periodic", job.Interval, 1)
		returned := false
		defer func() {
			if !returned {
				// Preserve the panic for Executor's existing Unknown outcome.
				// Never inspect or expose the handler's panic material.
				finish(errors.New("job handler did not return; result requires verification"))
				return
			}
			finish(err)
		}()
		err = run(ctx)
		returned = true
		return err
	}
	err := h.services.RegisterPeriodicJob(job)
	if err != nil {
		log.Printf("register job %s: %v", job.ID, err)
		return
	}
	bg.loops[job.ID] = &scheduledJob{}
}
func (h *handlers) closeScheduledJob(id string) {
	bg := h.backgroundJobs()
	h.services.RemoveJob(id)
	bg.mu.Lock()
	delete(bg.loops, id)
	bg.mu.Unlock()
	bg.observations.Waiting(id, "stopped", nil)
}

func (h *handlers) detachScheduledJob(id string) {
	bg := h.backgroundJobs()
	h.services.Jobs.Detach(id)
	bg.mu.Lock()
	delete(bg.loops, id)
	bg.mu.Unlock()
	bg.observations.Waiting(id, "stopped", nil)
}

// Shutdown stops this process's admission before waiting for local work. If the
// drain budget expires, Close interrupts local execution and still waits for its
// completion fences before file-spool ownership or database readers are closed.
// Explicit domain/plugin disable remains a separate global revocation action.
func (h *handlers) Shutdown(ctx context.Context) error {
	err := h.services.Jobs.Drain(ctx)
	h.CloseBackgroundJobs()
	return errors.Join(err, h.CloseZeroEventSpool())
}

func (h *handlers) CloseBackgroundJobs() {
	// Closing an unused adapter must not register handlers or start the pool.
	h.jobRuntimeOnce.Do(func() {
		h.jobRuntime = &backgroundJobs{observations: jobrun.New(), loops: map[string]*scheduledJob{}}
	})
	jobs := h.jobRuntime
	jobs.mu.Lock()
	ids := make([]string, 0, len(jobs.loops))
	for id := range jobs.loops {
		ids = append(ids, id)
	}
	jobs.mu.Unlock()
	// Process shutdown stops only this runtime. RemoveJob is a global handler
	// revocation: it cancels durable work owned by other processes as well.
	// Keep that operation for explicit domain/plugin disable, never scale-down.
	h.services.Close()
	for _, id := range ids {
		jobs.observations.Waiting(id, "stopped", nil)
	}
	jobs.mu.Lock()
	clear(jobs.loops)
	jobs.mu.Unlock()
}
