package datastore_test

import (
	"database/sql"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observabilitystore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"reflect"
	"testing"
	"time"
)

func TestSQLiteHistoryRetentionSeparatesPoliciesAndBoundsDescendants(t *testing.T) {
	historyRetentionPolicies(t, false)
}
func TestMySQLHistoryRetentionSeparatesPoliciesAndBoundsDescendants(t *testing.T) {
	historyRetentionPolicies(t, true)
}
func historyRetentionPolicies(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	ledger := diagnosticSchemaSource(t, db, false)
	if err := db.First(&ledger, ledger.ID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-200 * 24 * time.Hour)
	recent := now.Add(-15 * 24 * time.Hour)
	failed := now.Add(-45 * 24 * time.Hour)
	runs := make([]jobstore.Record, 2001)
	for i := range runs {
		runs[i] = jobstore.Record{ID: fmt.Sprintf("periodic-%d", i), Owner: "system", Key: fmt.Sprintf("periodic-%d", i), Handler: "retention-probe", ScheduleID: "retention-probe", State: "succeeded", Payload: "{}", CreatedAt: recent, NotBefore: recent, FinishedAt: &recent}
	}
	if err := db.CreateInBatches(&runs, 200).Error; err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		id, state string
		at        time.Time
		explicit  bool
	}{
		{"old-failure", "failed", failed, false}, {"old-canceled", "canceled", failed, false},
		{"recent-failure", "failed", recent, false}, {"unknown", "unknown", old, false},
		{"interrupted", "interrupted", old, false}, {"explicit-key", "succeeded", old, true}, {"current", "succeeded", old, false},
	} {
		schedule := "retention-probe"
		if probe.explicit {
			schedule = ""
		}
		row := jobstore.Record{ID: probe.id, Owner: "system", Key: probe.id, Handler: "retention-probe", ScheduleID: schedule, State: probe.state, Payload: "{}", CreatedAt: probe.at, NotBefore: probe.at, FinishedAt: &probe.at}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&jobstore.Schedule{ID: "retention-probe", RunID: "current", NextAt: now, State: "succeeded"}).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "email", Scope: "{}", Content: "{}", Status: 2, FinishedAt: &old, IdempotencyKey: "large-history"}
	held := model.Task{Type: "email", Scope: "{}", Content: "{}", Status: 3, FinishedAt: &old, IdempotencyKey: "uncertain-delivery"}
	for _, v := range []any{&task, &held} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	items := make([]model.TaskItem, 1503)
	for i := range items {
		items[i] = model.TaskItem{TaskID: task.ID, TargetType: "user", TargetID: fmt.Sprint(i), Payload: "{}", Status: 2}
	}
	if err := db.CreateInBatches(&items, 200).Error; err != nil {
		t.Fatal(err)
	}
	attempts := make([]model.MailDeliveryAttempt, len(items))
	for i, item := range items {
		attempts[i] = model.MailDeliveryAttempt{TaskID: task.ID, ItemID: item.ID, Attempt: 1, LeaseToken: fmt.Sprint(i), Acceptance: "accepted", StartedAt: old, FinishedAt: &old}
	}
	if err := db.CreateInBatches(&attempts, 200).Error; err != nil {
		t.Fatal(err)
	}
	heldItem := model.TaskItem{TaskID: held.ID, TargetType: "user", TargetID: "uncertain", Payload: "{}", Status: 3}
	if err := db.Create(&heldItem).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MailDeliveryAttempt{TaskID: held.ID, ItemID: heldItem.ID, Attempt: 1, LeaseToken: "unknown", Acceptance: "unknown", StartedAt: old, FinishedAt: &old}).Error; err != nil {
		t.Fatal(err)
	}
	noAdmission := func(tx *gorm.DB) {
		if tx.Statement.Table == "job_execution_budget" {
			tx.AddError(fmt.Errorf("retention acquired global job admission"))
		}
	}
	db.Callback().Query().Before("gorm:query").Register("retention:no_admission", noAdmission)
	defer db.Callback().Query().Remove("retention:no_admission")
	service := observability.HistoryRetention{Repository: observabilitystore.HistoryRetention{DB: db}}
	for round, want := range []int64{1000, 1000, 1} {
		result, err := service.Run(t.Context(), now)
		if err != nil || result.PeriodicSucceeded != want {
			t.Fatal("periodic batch bound/resume", round, result, err)
		}
		if round == 0 {
			if result.PeriodicFailed != 2 || result.Tasks != 0 {
				t.Fatal("mixed retention policy", result)
			}
			for _, v := range []any{&model.TaskItem{}, &model.MailDeliveryAttempt{}} {
				var count int64
				if err := db.Model(v).Where("task_id = ?", task.ID).Count(&count).Error; err != nil || count != 503 {
					t.Fatal("descendant budget not enforced", count, err)
				}
			}
		}
		if round == 1 && result.Tasks != 1 {
			t.Fatal("large terminal parent did not resume", result)
		}
	}
	for _, id := range []string{"recent-failure", "unknown", "interrupted", "explicit-key", "current"} {
		var count int64
		if err := db.Model(&jobstore.Record{}).Where("id = ?", id).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("durable/uncertain evidence expired", id, count, err)
		}
	}
	var count int64
	if err := db.Model(&model.MailDeliveryAttempt{}).Where("task_id = ?", held.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("uncertain delivery was age-pruned", err)
	}
	var after model.TrafficRecord
	if err := db.First(&after, ledger.ID).Error; err != nil || !reflect.DeepEqual(ledger, after) {
		t.Fatal("operational retention changed financial history", err)
	}
	if mysql {
		var plans []struct {
			Key   sql.NullString
			Extra string
		}
		if err := db.Raw("EXPLAIN SELECT id FROM job_runs WHERE state = ? AND finished_at < ? AND schedule_id <> '' ORDER BY finished_at,id LIMIT 1000", "succeeded", now).Scan(&plans).Error; err != nil {
			t.Fatal(err)
		}
		if len(plans) == 0 || !plans[0].Key.Valid || plans[0].Key.String != "idx_job_retention" {
			t.Fatal("retention does not seek its outcome/time index", plans)
		}
	}
}
