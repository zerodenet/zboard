package observabilitystore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAuditRechecksCurrentAdministrator(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "audit-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	service := observability.Audit{Repository: Audit{DB: db}}
	event := observability.AuditEvent{Action: "diagnostic.read", Target: "node:1", Detail: "result=ok"}
	if err := service.RecordAdmin(context.Background(), admin.ID, event); err != nil {
		t.Fatal(err)
	}
	var row model.AuditLog
	if err := db.First(&row).Error; err != nil || row.Actor != admin.Email || row.UserID == nil || *row.UserID != admin.ID {
		t.Fatalf("audit row = %+v err=%v", row, err)
	}
	if err := db.Model(&admin).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.RecordAdmin(context.Background(), admin.ID, event); !errors.Is(err, observability.ErrAuditPermission) {
		t.Fatalf("revoked administrator error = %v", err)
	}
}
