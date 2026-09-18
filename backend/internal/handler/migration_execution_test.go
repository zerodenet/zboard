package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type migrationCopyFunc func(context.Context, platform.MigrationTarget, uint, string) error

func (f migrationCopyFunc) Copy(ctx context.Context, target platform.MigrationTarget, id uint, run string) error {
	return f(ctx, target, id, run)
}

func TestMigrationExecutionRequiresClaimAndFinalizesUncertainty(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			if err := h.ReconcileSystemConfigDefaults(); err != nil {
				t.Fatal(err)
			}
			admin := model.User{Email: "migration-execution@example.test", Password: "unused", IsAdmin: true, Status: "active"}
			if err := h.db.Create(&admin).Error; err != nil {
				t.Fatal(err)
			}
			admission := h.services.DatabaseMigration(h.credentialCipher)
			admission.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error { return nil })
			driver := "mysql"
			if engine == "mysql" {
				driver = "sqlite"
			}
			input := platform.MigrationStart{Target: platform.MigrationTarget{TargetDriver: driver, TargetDataSource: "protected-destination"}, Confirm: true}
			store := jobstore.New(h.db)
			for _, mode := range []string{"panic", "cancel"} {
				accepted, err := admission.Start(context.Background(), admin.ID, input)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := store.List(context.Background(), "system", 100, 0)
				if err != nil {
					t.Fatal(err)
				}
				var queued jobs.Run
				for _, row := range rows {
					if row.ID == accepted.RunID {
						queued = row
					}
				}
				execution := h.services.MigrationExecution(h.credentialCipher)
				copied := false
				ctx, cancel := context.WithCancel(context.Background())
				execution.Copier = migrationCopyFunc(func(ctx context.Context, target platform.MigrationTarget, _ uint, _ string) error {
					copied = true
					if target.TargetDataSource != "protected-destination" {
						t.Error("target secret not decoded correctly")
					}
					if mode == "panic" {
						panic("simulated copy failure")
					}
					cancel()
					return ctx.Err()
				})
				if err := execution.Execute(ctx, queued); !errors.Is(err, jobs.ErrLeaseLost) {
					t.Fatal("unclaimed execution accepted", err)
				}
				var task model.Task
				if err := h.db.First(&task, accepted.TaskID).Error; err != nil || task.Status != taskStatusPending || copied {
					t.Fatal("unclaimed execution changed task", err)
				}
				executor := jobs.Executor{Store: store, Worker: "test", Timeout: time.Minute, Handlers: map[string]jobs.Handler{"database_migration": func(ctx context.Context, run jobs.Run) error {
					tampered := run
					tampered.Payload = strings.Replace(run.Payload, `"revision":"1"`, `"extra":"tampered","revision":"1"`, 1)
					if err := execution.Execute(ctx, tampered); !errors.Is(err, jobs.ErrLeaseLost) {
						t.Errorf("modified Run accepted: %v", err)
					}
					return execution.Execute(ctx, run)
				}}}
				err = executor.RunOne(ctx)
				cancel()
				if !errors.Is(err, jobs.ErrUncertain) || !copied {
					t.Fatal("uncertain execution not propagated", mode, err)
				}
				var record jobstore.Record
				if err := h.db.First(&record, "id = ?", accepted.RunID).Error; err != nil || record.State != string(jobs.Unknown) {
					t.Fatal("unknown result not retained", record.State, err)
				}
				if err := h.db.First(&task, accepted.TaskID).Error; err != nil || task.Status != taskStatusFailed || task.Content != "{}" || task.FinishedAt == nil {
					t.Fatal("task cleanup not committed", err)
				}
				var item model.TaskItem
				if err := h.db.Where("task_id = ?", task.ID).First(&item).Error; err != nil || item.Status != taskStatusFailed || item.FinishedAt == nil {
					t.Fatal("task item not finalized", err)
				}
				if _, err := store.Claim(context.Background(), "other", []string{"database_migration"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
					t.Fatal("uncertain work was replayed", err)
				}
				if err := h.services.JobReviews.Resolve(context.Background(), jobs.Reviewer{AccountID: admin.ID}, jobs.Review{RunID: accepted.RunID, Outcome: jobs.Failed, Reason: "test confirmed destination was not completed"}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
