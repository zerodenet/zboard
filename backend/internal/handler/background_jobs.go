package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/application"
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

var jobNames = map[string]string{"credential_expiry": "订阅到期处理", "fair_use": "公平使用评估", "certificate_renewal": "证书续期扫描", "dns_observation": "DNS 公网核验", "history_retention": "历史记录清理", "proxy_pool_sync": "代理池订阅同步", "node_publish": "节点配置发布", "admin_tasks": "运营任务执行", "registration_messages": "注册消息处理", "event_consumer": "流量事件入账"}

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
		if err != nil {
			message = sanitizeAuditDetail(truncateTaskError(err.Error()))
		}
		finish(message)
	}
}
func (h *handlers) startScheduledJob(id string, interval time.Duration, run func(context.Context) error) {
	bg := h.backgroundJobs()
	bg.mu.Lock()
	defer bg.mu.Unlock()
	if bg.loops[id] != nil {
		return
	}
	name := jobNames[id]
	if strings.HasPrefix(id, "node_publish_") {
		name = "节点配置发布 · " + strings.TrimPrefix(id, "node_publish_")
	}
	bg.observations.Register(id, name, "periodic", interval, 1)
	err := h.services.RegisterPeriodicJob(application.PeriodicJob{
		ID: id, Name: name, Interval: interval, Timeout: 30 * time.Minute, Immediate: true,
		External:           id == "admin_tasks" || strings.HasPrefix(id, "node_publish_"),
		ReconcileAfterLoss: id == "registration_messages" || id == "event_consumer" || id == "admin_tasks" || id == "credential_expiry" || id == "history_retention" || strings.HasPrefix(id, "node_publish_"),
		RetryFailures:      id == "registration_messages" || id == "event_consumer" || id == "admin_tasks" || id == "credential_expiry" || id == "history_retention" || strings.HasPrefix(id, "node_publish_"),
		Ready:              h.jobReadiness(id),
		Run: func(ctx context.Context) (err error) {
			finish := h.observeJob(id, "periodic", interval, 1)
			defer func() { finish(err) }()
			return run(ctx)
		},
	})
	if err != nil {
		log.Printf("register job %s: %v", id, err)
		return
	}
	bg.loops[id] = &scheduledJob{}
}
func (h *handlers) closeScheduledJob(id string) {
	bg := h.backgroundJobs()
	h.services.RemoveJob(id)
	bg.mu.Lock()
	delete(bg.loops, id)
	bg.mu.Unlock()
	bg.observations.Waiting(id, "stopped", nil)
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
	for _, id := range ids {
		h.closeScheduledJob(id)
	}
	h.services.Close()
}
