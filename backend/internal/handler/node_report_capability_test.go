package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestNodeReportCapabilityTransactionReplayAndRotation(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	if err := h.db.Model(&model.Node{}).Where("id = ?", credential.NodeID).Updates(map[string]interface{}{"is_enabled": true, "traffic_secret": "opaque-test-version", "traffic_secret_revoked_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	in := metering.AuthenticatedNodeReport{NodeID: credential.NodeID, UserID: credential.UserID, ProtocolEndpointID: credential.ProtocolEndpointID, ExpectedCredential: "opaque-test-version", Timestamp: time.Now().UTC(), Nonce: "test-nonce-000001", ReportID: "report:v1.0001", DownloadBytes: 10}
	service := h.services.NodeReports()
	failure := errors.New("quota event failed")
	repository := service.Repository.(meteringstore.NodeReports)
	repository.QuotaEvent = func(*gorm.DB, model.Subscription, string, int64, int64, int64, string, string) error { return failure }
	out, err := (metering.NodeReports{Repository: repository}).Record(context.Background(), in)
	if !errors.Is(err, failure) || out.Record.ID != 0 {
		t.Fatalf("failed transaction: %+v %v", out, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	var projected int64
	if err := h.db.Model(&model.ProtocolEndpointUsageDaily{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&projected).Error; err != nil || projected != 0 {
		t.Fatalf("failed report retained usage projection: %d %v", projected, err)
	}
	out, err = service.Record(context.Background(), in)
	if err != nil || out.Record.ID == 0 || out.Record.UsedBytes == 0 || out.Duplicate {
		t.Fatalf("report: %+v %v", out, err)
	}
	replay, err := service.Record(context.Background(), in)
	if err != nil || !replay.Duplicate || replay.Record.ID != out.Record.ID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	in.ReportID = "report:v1.0002"
	if _, err = service.Record(context.Background(), in); !errors.Is(err, metering.ErrNodeReportNonceReplayed) {
		t.Fatalf("nonce: %v", err)
	}
	in.Nonce = "test-nonce-000002"
	if err = h.db.Model(&model.Node{}).Where("id = ?", credential.NodeID).Update("traffic_secret", "rotated-test-version").Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Record(context.Background(), in); !errors.Is(err, metering.ErrNodeReportCredentialChanged) {
		t.Fatalf("rotation: %v", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, out.Record.UsedBytes, subStatusActive)
	if err := h.db.Model(&model.ProtocolEndpointUsageDaily{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&projected).Error; err != nil || projected != out.Record.UsedBytes {
		t.Fatalf("node report usage projection: %d %v", projected, err)
	}
}
