package networkstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProtocolEndpointOrderChecksAuthorityConflictAndAtomicAudit(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "endpoint-order.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "order-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []model.ProtocolEndpoint{{Name: "one", RuntimeKey: "one", Protocol: "vless", SortOrder: 0}, {Name: "two", RuntimeKey: "two", Protocol: "vmess", SortOrder: 1}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := network.ProtocolEndpointOrder{Repository: ProtocolEndpointOrder{DB: db}}
	initial, err := service.Read(context.Background(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, changed, err := service.Update(context.Background(), admin.ID, network.ProtocolEndpointOrderRequest{OrderedIDs: []uint{2, 1}, ExpectedVersion: initial.Version})
	if err != nil || !changed || updated.Items[0].ID != 2 {
		t.Fatalf("updated=%+v changed=%t err=%v", updated, changed, err)
	}
	if _, _, err := service.Update(context.Background(), admin.ID, network.ProtocolEndpointOrderRequest{OrderedIDs: []uint{1, 2}, ExpectedVersion: initial.Version}); !errors.Is(err, network.ErrProtocolEndpointOrderConflict) {
		t.Fatalf("stale version error = %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_endpoint_order_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'protocol_endpoint.order' BEGIN SELECT RAISE(ABORT, 'audit failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Update(context.Background(), admin.ID, network.ProtocolEndpointOrderRequest{OrderedIDs: []uint{1, 2}, ExpectedVersion: updated.Version}); err == nil {
		t.Fatal("audit failure accepted")
	}
	after, err := service.Read(context.Background(), admin.ID)
	if err != nil || after.Items[0].ID != 2 {
		t.Fatalf("order changed outside audit transaction: %+v err=%v", after, err)
	}
	if err := db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Read(context.Background(), admin.ID); !errors.Is(err, network.ErrProtocolEndpointOrderPermission) {
		t.Fatalf("revoked administrator error = %v", err)
	}
}
