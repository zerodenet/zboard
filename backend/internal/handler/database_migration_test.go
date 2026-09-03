package handler

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestReconcileInterruptedDatabaseMigrationsFailsTaskAndKeepsMaintenance(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "zboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := model.Task{Type: taskTypeDatabaseMigration, Scope: `{}`, Content: "encrypted-target", Status: taskStatusRunning, Total: 3, MaxAttempts: 1, StartedAt: &now}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: task.ID, TargetType: "database", TargetID: "mysql", Status: taskStatusRunning}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	maintenance := model.SystemConfig{ConfigKey: "maintenance_enabled", Name: "maintenance", Value: "true", ValueType: "bool", Revision: 1}
	if err := db.Create(&maintenance).Error; err != nil {
		t.Fatal(err)
	}
	h := &handlers{db: db}
	if err := h.ReconcileInterruptedDatabaseMigrations(); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != taskStatusFailed || task.Content != "" || !strings.Contains(task.Errors, "interrupted") {
		t.Fatalf("reconciled task = %+v", task)
	}
	if err := db.First(&maintenance, maintenance.ID).Error; err != nil {
		t.Fatal(err)
	}
	if maintenance.Value != "true" {
		t.Fatalf("maintenance value = %q, want true", maintenance.Value)
	}
}
