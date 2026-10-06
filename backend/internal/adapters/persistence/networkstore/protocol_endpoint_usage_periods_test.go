package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProtocolEndpointUsageResetStartsNewPeriodWithoutChangingAccounting(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	admin := model.User{ID: 1, Email: "admin@example.test", Status: "active", IsAdmin: true}
	node := model.Node{ID: 3, Name: "node", Address: "node.example.test", Config: "{}"}
	endpoint := model.ProtocolEndpoint{ID: 7, NodeID: node.ID, Name: "direct", Protocol: "vless", Address: node.Address, Port: 443, PublicPort: 443, ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	for _, row := range []any{&admin, &node, &endpoint} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	usage := model.ProtocolEndpointUsageDaily{ProtocolEndpointID: endpoint.ID, UsageDate: "2026-09-30", UsedBytes: 150, LastRecordAt: now, UpdatedAt: now}
	ledger := model.TrafficRecord{NodeID: node.ID, ProtocolEndpointID: endpoint.ID, ReportID: "first", Nonce: "first", UsedBytes: 150, At: now}
	for _, row := range []any{&usage, &ledger} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	store := ProtocolEndpointUsagePeriods{DB: db}
	periods := network.ProtocolEndpointUsagePeriods{Repository: store}
	// Keep the reset boundary on the same clock as the usage fixture. The public
	// capability deliberately uses wall time, which is not this test's date.
	first, err := store.ResetProtocolEndpointUsage(context.Background(), admin.ID, endpoint.ID, "更换服务器", now)
	if err != nil || first.PeriodUsedBytes != 150 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	read := Inventory{DB: db}
	assertUsage := func(wantToday, wantTotal int64) {
		t.Helper()
		got, err := read.ProtocolUsage(context.Background(), []uint{endpoint.ID}, now)
		if err != nil {
			t.Fatal(err)
		}
		if got[endpoint.ID].UsedBytesToday != wantToday || got[endpoint.ID].UsedBytesTotal != wantTotal || got[endpoint.ID].ResetAt == nil {
			t.Fatalf("usage=%+v want today=%d total=%d", got[endpoint.ID], wantToday, wantTotal)
		}
	}
	assertUsage(0, 0)
	if err := db.Model(&usage).Updates(map[string]any{"used_bytes": 190, "record_count": 2}).Error; err != nil {
		t.Fatal(err)
	}
	assertUsage(40, 40)
	second, err := store.ResetProtocolEndpointUsage(context.Background(), admin.ID, endpoint.ID, "更换端口", now)
	if err != nil || second.PeriodUsedBytes != 40 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	assertUsage(0, 0)
	nextDay := now.Add(24 * time.Hour)
	if err := db.Create(&model.ProtocolEndpointUsageDaily{ProtocolEndpointID: endpoint.ID, UsageDate: nextDay.Format("2006-01-02"), UsedBytes: 20, LastRecordAt: nextDay, UpdatedAt: nextDay}).Error; err != nil {
		t.Fatal(err)
	}
	acrossMidnight, err := read.ProtocolUsage(context.Background(), []uint{endpoint.ID}, nextDay)
	if err != nil || acrossMidnight[endpoint.ID].UsedBytesToday != 20 || acrossMidnight[endpoint.ID].UsedBytesTotal != 20 {
		t.Fatalf("next day usage=%+v err=%v", acrossMidnight[endpoint.ID], err)
	}
	history, err := periods.History(context.Background(), admin.ID, endpoint.ID)
	if err != nil || len(history) != 2 || history[0].ID != second.ID || history[1].ID != first.ID {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	var stored model.ProtocolEndpointUsageDaily
	if err := db.First(&stored, "protocol_endpoint_id = ? AND usage_date = ?", endpoint.ID, now.Format("2006-01-02")).Error; err != nil || stored.UsedBytes != 190 {
		t.Fatalf("daily=%+v err=%v", stored, err)
	}
	var count int64
	if err := db.Model(&model.TrafficRecord{}).Where("protocol_endpoint_id = ?", endpoint.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("ledger count=%d err=%v", count, err)
	}
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "protocol_endpoint.usage_reset").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
}

func TestProtocolEndpointUsageResetRequiresAdminAndReason(t *testing.T) {
	db, _ := administrationFixture(t)
	service := network.ProtocolEndpointUsagePeriods{Repository: ProtocolEndpointUsagePeriods{DB: db}}
	_, err := service.Reset(context.Background(), 1, 2, " ")
	var validation *network.ProtocolEndpointUsagePeriodValidation
	if !errors.As(err, &validation) {
		t.Fatalf("error=%v", err)
	}
	_, err = service.Reset(context.Background(), 2, 2, "reason")
	if !errors.Is(err, network.ErrProtocolEndpointUsagePermission) {
		t.Fatalf("error=%v", err)
	}
}
