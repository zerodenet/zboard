package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNodeReportDiagnosticSizeDoesNotBlockAccounting(t *testing.T) {
	assertNodeReportDiagnosticSize(t, newOrderFixture(t))
}

func TestMySQLNodeReportDiagnosticSizeDoesNotBlockAccounting(t *testing.T) {
	assertNodeReportDiagnosticSize(t, newMySQLBusinessOrderFixture(t))
}

func assertNodeReportDiagnosticSize(t *testing.T, f orderFixture) {
	t.Helper()
	h, credential := accountingFixtureFromOrder(t, f)
	if err := h.db.Model(&model.Node{}).Where("id = ?", credential.NodeID).Update("traffic_secret", "diagnostic-fixture-key").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", credential.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	service := h.services.NodeReports()
	metas := []string{strings.Repeat("m", 65535), strings.Repeat("m", 65536), strings.Repeat("诊", 24000)}
	for i, meta := range metas {
		in := metering.AuthenticatedNodeReport{NodeID: credential.NodeID, UserID: credential.UserID, ProtocolEndpointID: credential.ProtocolEndpointID,
			ExpectedCredential: "diagnostic-fixture-key", Timestamp: time.Now().UTC(), Nonce: fmt.Sprintf("diagnostic-nonce-%d", i), ReportID: fmt.Sprintf("diagnostic-report-%d", i), DownloadBytes: 3, Meta: meta}
		out, err := service.Record(t.Context(), in)
		if err != nil || out.Duplicate || out.Record.RawBytes != 3 || out.Record.UsedBytes != 5 || out.Record.ProtocolMultiplierMilli != 1500 {
			t.Fatalf("optional diagnostic blocked/changed billing: used=%d raw=%d duplicate=%t err=%v", out.Record.UsedBytes, out.Record.RawBytes, out.Duplicate, err)
		}
		if i == 0 {
			if out.Record.Meta != meta {
				t.Fatal("existing TEXT-sized diagnostics changed")
			}
		} else {
			var receipt struct {
				Type          string `json:"type"`
				Version       int    `json:"version"`
				Reason        string `json:"reason"`
				OriginalBytes int    `json:"original_bytes"`
				MaxBytes      int    `json:"max_bytes"`
				SHA256        string `json:"sha256"`
			}
			original := sha256.Sum256([]byte(meta))
			if err := json.Unmarshal([]byte(out.Record.Meta), &receipt); err != nil || len(out.Record.Meta) > 512 || receipt.Type != "diagnostic_meta_omitted" || receipt.Version != 1 || receipt.Reason != "text_byte_limit" || receipt.OriginalBytes != len(meta) || receipt.MaxBytes != 65535 || receipt.SHA256 != hex.EncodeToString(original[:]) {
				t.Fatalf("diagnostic omission lacks bounded audit receipt: stored_bytes=%d err=%v", len(out.Record.Meta), err)
			}
		}
		for range 2 {
			replay, err := service.Record(t.Context(), in)
			if err != nil || !replay.Duplicate || replay.Record.ID != out.Record.ID || replay.Record.Meta != out.Record.Meta {
				t.Fatalf("diagnostic retry changed receipt/accounting: duplicate=%t err=%v", replay.Duplicate, err)
			}
		}
		assertAccountingTotal(t, h, credential.SubscriptionID, int64(i+1)*5, subStatusActive)
	}
	// A reliable projection intent is still part of financial commit. Its failure
	// must roll back the ledger, nonce and quota even with a large diagnostic.
	trigger := `CREATE TRIGGER reject_diagnostic_projection BEFORE INSERT ON metering_usage_projection_pending BEGIN SELECT RAISE(ABORT, 'intent unavailable'); END`
	if !datastore.IsSQLite(h.db) {
		trigger = `CREATE TRIGGER reject_diagnostic_projection BEFORE INSERT ON metering_usage_projection_pending FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'intent unavailable'`
	}
	if err := h.db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	in := metering.AuthenticatedNodeReport{NodeID: credential.NodeID, UserID: credential.UserID, ProtocolEndpointID: credential.ProtocolEndpointID,
		ExpectedCredential: "diagnostic-fixture-key", Timestamp: time.Now().UTC(), Nonce: "diagnostic-rollback-nonce", ReportID: "diagnostic-rollback-report", DownloadBytes: 3, Meta: metas[2]}
	if _, err := service.Record(t.Context(), in); err == nil {
		t.Fatal("failed durable intent acknowledged")
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 15, subStatusActive)
	if err := h.db.Exec("DROP TRIGGER reject_diagnostic_projection").Error; err != nil {
		t.Fatal(err)
	}
	if out, err := service.Record(t.Context(), in); err != nil || out.Duplicate || out.Record.UsedBytes != 5 {
		t.Fatalf("explicit retry after rollback failed: duplicate=%t err=%v", out.Duplicate, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 20, subStatusActive)
	drainUsageProjections(t, h.db)
	var projected int64
	if err := h.db.Model(&model.ProtocolEndpointUsageDaily{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&projected).Error; err != nil || projected != 20 {
		t.Fatalf("diagnostics changed independent projection: %d %v", projected, err)
	}
}
