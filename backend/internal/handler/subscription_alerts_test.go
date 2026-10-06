package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func alertFixture(t *testing.T) (*handlers, messagingstore.SubscriptionAlerts, model.Subscription, time.Time) {
	t.Helper()
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.ReconcileSystemConfigDefaults(); err != nil {
		t.Fatal(err)
	}
	for _, row := range []model.SystemConfig{
		{ConfigKey: "task_email_enabled", Value: "true", ValueType: "bool"},
		{ConfigKey: "smtp_host", Value: "smtp.example.test", ValueType: "string"},
		{ConfigKey: "smtp_port", Value: "587", ValueType: "int"},
		{ConfigKey: "smtp_from", Value: "sender@example.test", ValueType: "string"},
		{ConfigKey: "smtp_tls_mode", Value: "starttls", ValueType: "string"},
	} {
		if err := h.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := h.db.Create(&model.Installation{ID: 1, SiteName: "Alert fixture", SiteURL: "https://site.example", InstalledAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sub := model.Subscription{UserID: 1, Status: "active", StartAt: now.Add(-time.Hour), EndAt: now.Add(30 * 24 * time.Hour), FlowTotal: 100, FlowUsed: 90, Config: "{}"}
	if err := h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	return h, messagingstore.SubscriptionAlerts{DB: h.db, Cipher: h.credentialCipher}, sub, now
}
func setAlertConfig(t *testing.T, h *handlers, key, value string) {
	t.Helper()
	if err := h.db.Model(&model.SystemConfig{}).Where("config_key = ?", key).Update("value", value).Error; err != nil {
		t.Fatal(err)
	}
}
func processAlerts(t *testing.T, s messagingstore.SubscriptionAlerts, now time.Time, limit, want int) {
	t.Helper()
	got, err := s.Process(context.Background(), now, limit)
	if err != nil || got != want {
		t.Fatalf("created %d want %d: %v", got, want, err)
	}
}

func TestSubscriptionAlertsDedupSurvivesRestartAndPruning(t *testing.T) {
	h, store, sub, now := alertFixture(t)
	processAlerts(t, store, now, 200, 0)
	setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
	processAlerts(t, store, now, 200, 1)
	var task model.Task
	h.db.First(&task)
	var payload messaging.EmailContent
	if err := json.Unmarshal([]byte(task.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Alert == nil || payload.Alert.Kind != "low" || payload.SiteName != "Alert fixture" || task.ScheduledAt == nil {
		t.Fatal("incomplete durable alert", payload, task)
	}
	setAlertConfig(t, h, "subscription_alert_low_enabled", "false")
	setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
	if err := h.db.Delete(&model.TaskItem{}, "task_id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Delete(&task).Error; err != nil {
		t.Fatal(err)
	}
	store = messagingstore.SubscriptionAlerts{DB: h.db, Cipher: h.credentialCipher}
	processAlerts(t, store, now.Add(25*time.Hour), 200, 0)
	// A purchased reset advances the cumulative baseline and starts a new cycle.
	if err := h.db.Model(&sub).Updates(map[string]any{"cycle_start_used": 90, "flow_total": 190, "flow_used": 180}).Error; err != nil {
		t.Fatal(err)
	}
	processAlerts(t, store, now.Add(25*time.Hour), 200, 1)
	var count int64
	h.db.Model(&model.SubscriptionAlert{}).Count(&count)
	if count != 2 {
		t.Fatal("incorrect retained dedup ledger", count)
	}
}

func TestSubscriptionAlertsShareAccountCooldownAndPageCursor(t *testing.T) {
	h, store, sub, now := alertFixture(t)
	setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
	second := sub
	second.ID = 0
	if err := h.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	processAlerts(t, store, now, 1, 1)
	// A new instance resumes at the next subscription, but its owner is throttled.
	store = messagingstore.SubscriptionAlerts{DB: h.db, Cipher: h.credentialCipher}
	processAlerts(t, store, now, 1, 0)
	processAlerts(t, store, now, 1, 0) // wrap
	processAlerts(t, store, now.Add(25*time.Hour), 1, 0)
	processAlerts(t, store, now.Add(25*time.Hour), 1, 1)
	var last model.SubscriptionAlert
	h.db.Last(&last)
	if last.SubscriptionID != second.ID {
		t.Fatal("newer subscription starved", last)
	}
}

func TestSubscriptionAlertQueueAndAuditRollbackTogether(t *testing.T) {
	h, store, _, now := alertFixture(t)
	setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
	if err := h.db.Exec(`CREATE TRIGGER reject_alert_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'notification.enqueue' BEGIN SELECT RAISE(ABORT, 'fixture'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Process(context.Background(), now, 200); err == nil {
		t.Fatal("failed audit committed")
	}
	for _, entity := range []any{&model.Task{}, &model.TaskItem{}, &model.SubscriptionAlert{}} {
		var n int64
		h.db.Model(entity).Count(&n)
		if n != 0 {
			t.Fatal("partial alert persisted", n)
		}
	}
	if err := h.db.Exec("DROP TRIGGER reject_alert_audit").Error; err != nil {
		t.Fatal(err)
	}
	processAlerts(t, store, now, 200, 1)
}

func TestSubscriptionAlertsRequireSMTPAndAdministrator(t *testing.T) {
	h, store, _, now := alertFixture(t)
	setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
	setAlertConfig(t, h, "smtp_host", "")
	if _, err := store.Process(context.Background(), now, 200); err == nil {
		t.Fatal("broken mail channel consumed alert")
	}
	setAlertConfig(t, h, "task_email_enabled", "false")
	processAlerts(t, store, now, 200, 0)
	in := platform.SettingUpdateInput{Key: "subscription_alert_remaining_percent", Value: json.RawMessage(`10`)}
	if _, err := h.services.SettingUpdate(h.credentialCipher).Update(context.Background(), 1, in); !errors.Is(err, platform.ErrSettingsPermission) {
		t.Fatal("subscriber can change alerts", err)
	}
	admin := model.User{Email: "admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	revision := uint64(1)
	in.ExpectedRevision = &revision
	view, err := h.services.SettingUpdate(h.credentialCipher).Update(context.Background(), admin.ID, in)
	if err != nil || view.Revision != 2 {
		t.Fatal(view, err)
	}
	if _, err := h.services.SettingUpdate(h.credentialCipher).Update(context.Background(), admin.ID, in); !errors.Is(err, platform.ErrSettingRevision) {
		t.Fatal("stale admin write accepted", err)
	}
	in.ExpectedRevision = nil
	in.Value = json.RawMessage(`91`)
	if _, err := h.services.SettingUpdate(h.credentialCipher).Update(context.Background(), admin.ID, in); err == nil {
		t.Fatal("invalid threshold accepted")
	}
}

type alertBusinessFunc func(context.Context, jobs.BatchReceipt, jobs.BatchItem) error

func (f alertBusinessFunc) ExecuteBatchBusiness(ctx context.Context, b jobs.BatchReceipt, i jobs.BatchItem) error {
	return f(ctx, b, i)
}

func TestSubscriptionAlertsRevalidateAndRecordSuppression(t *testing.T) {
	for _, kind := range []string{"disabled", "quota recovered", "cancelled", "reset", "renewed", "recipient disabled", "global mail off", "valid"} {
		t.Run(kind, func(t *testing.T) {
			h, store, sub, now := alertFixture(t)
			setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
			if kind == "renewed" {
				setAlertConfig(t, h, "subscription_alert_low_enabled", "false")
				setAlertConfig(t, h, "subscription_alert_expiring_enabled", "true")
				if err := h.db.Model(&sub).Update("end_at", now.Add(time.Hour)).Error; err != nil {
					t.Fatal(err)
				}
			}
			processAlerts(t, store, now, 200, 1)
			var task model.Task
			var item model.TaskItem
			h.db.First(&task)
			h.db.First(&item)
			switch kind {
			case "disabled":
				setAlertConfig(t, h, "subscription_alert_low_enabled", "false")
			case "global mail off":
				setAlertConfig(t, h, "task_email_enabled", "false")
			case "quota recovered":
				h.db.Model(&sub).Update("flow_used", 0)
			case "cancelled":
				h.db.Model(&sub).Update("status", "cancelled")
			case "reset":
				h.db.Model(&sub).Updates(map[string]any{"cycle_start_used": 90, "flow_total": 190, "flow_used": 180})
			case "renewed":
				h.db.Model(&sub).Update("end_at", now.Add(30*24*time.Hour))
			case "recipient disabled":
				h.db.Model(&model.User{}).Where("id = ?", sub.UserID).Update("status", "suspended")
			}
			token, err := h.services.ClaimBatch(context.Background(), 0, task.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			service := messaging.EmailExecution{Repository: messagingstore.EmailExecution{DB: h.db}, Sender: taskMailFunc(func(_ context.Context, m messaging.Message) error {
				calls++
				if !strings.Contains(m.Subject, "Alert fixture") || m.Recipient != "reader@example.test" {
					t.Fatal(m)
				}
				return nil
			})}
			runner := h.services.BatchItemRunner(alertBusinessFunc(func(ctx context.Context, b jobs.BatchReceipt, i jobs.BatchItem) error {
				return service.Execute(ctx, messaging.EmailExecutionClaim{TaskID: b.ID, ItemID: i.ID, Token: token})
			}))
			failures := runner.Run(context.Background(), jobs.BatchItemClaim{TaskID: task.ID, ItemID: item.ID, Token: token})
			if err := h.services.BatchLifecycle().Finish(context.Background(), task.ID, token, failures); err != nil {
				t.Fatal(err)
			}
			h.db.First(&item, item.ID)
			h.db.First(&task, task.ID)
			var attempt model.MailDeliveryAttempt
			h.db.First(&attempt)
			switch kind {
			case "valid":
				if calls != 1 || item.DeliveryState != "accepted" || attempt.Acceptance != "accepted" {
					t.Fatal("valid alert not sent", calls, item, attempt)
				}
			case "recipient disabled":
				if calls != 0 || task.Status != 3 {
					t.Fatal("inactive account sent", calls, task)
				}
			default:
				if calls != 0 || task.Status != 2 || item.DeliveryState != "suppressed" || item.Error == "" || attempt.Acceptance != "not_accepted" {
					t.Fatal("incorrect suppression", calls, task, item, attempt)
				}
			}
		})
	}
}

func TestSubscriptionAlertDeliveryCooldownStopsDelayedQueueBurst(t *testing.T) {
	h, store, sub, now := alertFixture(t)
	setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
	if err := h.db.Model(&sub).Update("start_at", now.Add(-48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	processAlerts(t, store, now.Add(-25*time.Hour), 200, 1)
	second := sub
	second.ID = 0
	if err := h.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	processAlerts(t, store, now, 200, 1)
	var tasks []model.Task
	h.db.Order("id").Find(&tasks)
	for index, task := range tasks {
		var item model.TaskItem
		h.db.Where("task_id = ?", task.ID).First(&item)
		until := now.Add(time.Minute)
		if err := h.db.Model(&task).Updates(map[string]any{"status": 1, "locked_by": "lease", "locked_until": until}).Error; err != nil {
			t.Fatal(err)
		}
		if err := h.db.Model(&item).Update("status", 1).Error; err != nil {
			t.Fatal(err)
		}
		_, err := (messagingstore.EmailExecution{DB: h.db}).Prepare(context.Background(), messaging.EmailExecutionClaim{TaskID: task.ID, ItemID: item.ID, Token: "lease"})
		if index == 0 && err != nil {
			t.Fatal(err)
		}
		if index == 1 && !errors.Is(err, messaging.ErrAlertSuppressed) {
			t.Fatal("delayed queue burst allowed", err)
		}
	}
}

func TestSubscriptionAlertParallelScanDoesNotDuplicate(t *testing.T) {
	h, store, _, now := alertFixture(t)
	setAlertConfig(t, h, "subscription_alert_low_enabled", "true")
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := store.Process(context.Background(), now, 200); errs <- err }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil && !strings.Contains(err.Error(), "locked") {
			t.Fatal(err)
		}
	}
	processAlerts(t, store, now, 200, 0)
	var count int64
	h.db.Model(&model.SubscriptionAlert{}).Count(&count)
	if count != 1 {
		t.Fatal(fmt.Sprintf("parallel scans created %d alerts", count))
	}
}
