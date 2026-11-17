package handler

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestObservationIntentControlledRetry(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	assertObservationControlledRetry(t, h)
}
func TestMySQLObservationIntentControlledRetry(t *testing.T) {
	h, _ := newMySQLBusinessHandlers(t)
	assertObservationControlledRetry(t, h)
}
func assertObservationControlledRetry(t *testing.T, h *handlers) {
	t.Helper()
	admin := model.User{Email: "observation-retry-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	ordinary := model.User{Email: "observation-retry-user@example.test", Password: "unused", Status: "active"}
	for _, u := range []*model.User{&admin, &ordinary} {
		if err := h.db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	token := func(u model.User) string {
		v, _, err := h.issueToken(authClaims{UserID: u.ID, Email: u.Email, IsAdmin: true})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	adminToken, userToken := token(admin), token(ordinary)
	held := func(label string) (model.Node, model.NodeObservationIntent) {
		t.Helper()
		node := model.Node{Name: label, Config: "{}", IsEnabled: true}
		if err := h.db.Create(&node).Error; err != nil {
			t.Fatal(err)
		}
		if err := h.db.Model(&node).Update("is_enabled", false).Error; err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC().Add(-time.Hour)
		p := network.EventPosition{AssetTag: node.AssetTag, ID: label, CoreInstanceID: "retry-test-core", SourceID: "retry-test-source", Sequence: 1, OccurredAt: at, ReceivedAt: at}
		source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p, StatsEvent: &p, Stats: network.NodeStats{ActiveSessions: 7, BytesUp: 100, BytesDown: 200}}, Kind: "stats.sampled", Payload: []byte(`{"active_sessions":7,"bytes_up":100,"bytes_down":200}`)}
		var id string
		if err := h.db.Transaction(func(tx *gorm.DB) error { var e error; id, e = observationstore.Enqueue(tx, source); return e }); err != nil {
			t.Fatal(err)
		}
		if _, err := h.services.NodeObservations.Drain(t.Context(), 32, 8*1024*1024); err != nil {
			t.Fatal(err)
		}
		var header model.NodeObservationIntent
		if err := h.db.First(&header, "id = ?", id).Error; err != nil || header.Status != "held" || header.Reason != "node_inactive" || header.Attempts != 1 {
			t.Fatalf("inactive material %+v %v", header, err)
		}
		return node, header
	}
	node, header := held("restore-original-observation")
	in := network.ObservationIntentRetryRequest{ExpectedAttempts: "1", Reason: strings.Repeat("r", 400)}
	request := func(credential, id, body string, status int) network.ObservationIntentRetryReceipt {
		t.Helper()
		w := httptest.NewRecorder()
		h.NodeObservationIntentRetryHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/node-observation-intents/"+id+"/retry", credential, body))
		if w.Code != status {
			t.Fatalf("retry HTTP %d want%d %s", w.Code, status, w.Body.String())
		}
		if strings.Contains(w.Body.String(), node.AssetTag) || strings.Contains(w.Body.String(), "retry-test-core") {
			t.Fatalf("private source exposed: %s", w.Body.String())
		}
		var out struct {
			Data network.ObservationIntentRetryReceipt
		}
		if status == http.StatusAccepted {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out.Data
	}
	raw, _ := json.Marshal(in)
	request(userToken, header.ID, string(raw), http.StatusForbidden)
	request(adminToken, header.ID, string(raw), http.StatusConflict)
	for _, body := range []string{`{"expected_attempts":"01","reason":"test"}`, `{"expected_attempts":1,"reason":"test"}`, `{"expected_attempts":"18446744073709551615","reason":"test"}`, `{"expected_attempts":"1","reason":"test","force":true}`, `{"expected_attempts":"1","reason":"test"} {}`} {
		request(adminToken, header.ID, body, http.StatusBadRequest)
	}
	if err := h.db.Model(&node).Update("is_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	var partsBefore []model.NodeObservationPart
	if err := h.db.Where("intent_id = ?", header.ID).Order("ordinal").Find(&partsBefore).Error; err != nil {
		t.Fatal(err)
	}
	trigger := `CREATE TRIGGER reject_observation_retry_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'node.observation.retry' BEGIN SELECT RAISE(ABORT, 'retry audit unavailable'); END`
	if !datastore.IsSQLite(h.db) {
		trigger = `CREATE TRIGGER reject_observation_retry_audit BEFORE INSERT ON audit_logs FOR EACH ROW BEGIN IF NEW.action = 'node.observation.retry' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'retry audit unavailable'; END IF; END`
	}
	if err := h.db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	request(adminToken, header.ID, string(raw), http.StatusInternalServerError)
	var rollback model.NodeObservationIntent
	if err := h.db.First(&rollback, "id = ?", header.ID).Error; err != nil || rollback.Status != "held" || rollback.Reason != "node_inactive" || rollback.Attempts != header.Attempts || rollback.Token != "" {
		t.Fatalf("unaudited requeue committed %+v %v", rollback, err)
	}
	if err := h.db.Exec("DROP TRIGGER reject_observation_retry_audit").Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	receipts := make(chan network.ObservationIntentRetryReceipt, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := h.services.ObservationIntents.Retry(t.Context(), admin.ID, header.ID, in)
			receipts <- r
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	close(receipts)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, network.ErrObservationIntentRetryConflict) {
			conflict++
		} else {
			t.Fatalf("retry race %v", err)
		}
	}
	if success != 1 || conflict != 7 {
		t.Fatalf("candidate admitted %d times, conflicts%d", success, conflict)
	}
	var receipt network.ObservationIntentRetryReceipt
	for r := range receipts {
		if r.ID != "" {
			receipt = r
		}
	}
	if receipt.Status != "accepted" || receipt.ID != header.ID || receipt.ExpectedAttempts != "1" || receipt.AuditID == 0 {
		t.Fatalf("retry receipt %+v", receipt)
	}
	var audit model.AuditLog
	if err := h.db.First(&audit, receipt.AuditID).Error; err != nil || audit.UserID == nil || *audit.UserID != admin.ID || audit.Actor != admin.Email || !strings.Contains(audit.Detail, in.Reason) {
		t.Fatalf("retry audit %+v %v", audit, err)
	}
	var auditCount int64
	if err := h.db.Model(&model.AuditLog{}).Where("action = ?", "node.observation.retry").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("audit repeated %d %v", auditCount, err)
	}
	var pending model.NodeObservationIntent
	if err := h.db.First(&pending, "id = ?", header.ID).Error; err != nil || pending.Status != "pending" || pending.Attempts != 1 || pending.Token != "" || pending.MaterialBytes != header.MaterialBytes || pending.PartCount != header.PartCount {
		t.Fatalf("pending original source %+v %v", pending, err)
	}
	var partsAfter []model.NodeObservationPart
	if err := h.db.Where("intent_id = ?", header.ID).Order("ordinal").Find(&partsAfter).Error; err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(partsBefore)
	b, _ := json.Marshal(partsAfter)
	if string(a) != string(b) {
		t.Fatal("retry changed immutable material")
	}
	// A fresh executor consumes the persisted intent; this is a restart of the
	// worker object, not a claim of an OS/process crash or capacity acceptance.
	if done, err := (observationstore.Worker{DB: h.db}).Drain(t.Context(), 32, 8*1024*1024); err != nil || done != 1 {
		t.Fatalf("fresh worker did not recover %d %v", done, err)
	}
	if err := h.db.First(&node, node.ID).Error; err != nil || node.ActiveFlows != 7 || node.BytesUp != 100 || node.BytesDown != 200 {
		t.Fatalf("recovered counters %+v %v", node, err)
	}
	var applied model.NodeObservationIntent
	if err := h.db.First(&applied, "id = ?", header.ID).Error; err != nil || applied.Status != "applied" || applied.Attempts != 2 {
		t.Fatalf("worker bypassed ownership %+v %v", applied, err)
	}
	request(adminToken, header.ID, string(raw), http.StatusConflict)
	for _, kind := range []string{"asset_mismatch", "asset_deleted", "credential_revoked", "invalid_material", "node_deleted", "active_claim"} {
		t.Run(kind, func(t *testing.T) {
			n, hdr := held(kind)
			if err := h.db.Model(&n).Update("is_enabled", true).Error; err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "asset_mismatch":
				// Asset labels are create-only. Exercise real numeric-ID reuse;
				// a model Update would correctly ignore an attempted relabel.
				if err := h.db.Delete(&n).Error; err != nil {
					t.Fatal(err)
				}
				replacement := model.Node{ID: n.ID, Name: "replacement-original-ID", Config: "{}", IsEnabled: true}
				if err := h.db.Create(&replacement).Error; err != nil {
					t.Fatal(err)
				}
				if replacement.AssetTag == hdr.AssetTag {
					t.Fatal("replacement retained old asset identity")
				}
			case "asset_deleted":
				if err := h.db.Transaction(func(tx *gorm.DB) error { return observationstore.Tombstone(tx, n.ID, n.AssetTag) }); err != nil {
					t.Fatal(err)
				}
			case "credential_revoked":
				if err := h.db.Model(&n).Update("node_credential_revoked_at", time.Now().UTC()).Error; err != nil {
					t.Fatal(err)
				}
			case "invalid_material":
				if err := h.db.Model(&model.NodeObservationIntent{}).Where("id = ?", hdr.ID).Update("reason", "invalid_material").Error; err != nil {
					t.Fatal(err)
				}
			case "active_claim":
				if err := h.db.Model(&model.NodeObservationIntent{}).Where("id = ?", hdr.ID).Updates(map[string]any{"status": "claimed", "token": "active-owner-token", "lease_until": time.Now().UTC().Add(time.Hour)}).Error; err != nil {
					t.Fatal(err)
				}
			case "node_deleted":
				if err := h.db.Transaction(func(tx *gorm.DB) error {
					if err := observationstore.Tombstone(tx, n.ID, n.AssetTag); err != nil {
						return err
					}
					return tx.Delete(&n).Error
				}); err != nil {
					t.Fatal(err)
				}
			}
			_, err := h.services.ObservationIntents.Retry(t.Context(), admin.ID, hdr.ID, in)
			if !errors.Is(err, network.ErrObservationIntentRetryConflict) {
				t.Fatalf("unsafe %s retry %v", kind, err)
			}
			var retained model.NodeObservationIntent
			wantStatus := "held"
			if kind == "active_claim" {
				wantStatus = "claimed"
			}
			if err := h.db.First(&retained, "id = ?", hdr.ID).Error; err != nil || retained.Status != wantStatus || retained.Attempts != 1 || kind == "active_claim" && retained.Token != "active-owner-token" {
				t.Fatalf("unsafe header changed %+v %v", retained, err)
			}
		})
	}
	// Acceptance is not material verification. Preserve a corruption discovered
	// by the original worker after explicit requeue, without updating counters.
	corruptNode, corruptHeader := held("corrupt-material-after-queue")
	if err := h.db.Model(&corruptNode).Update("is_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.NodeObservationPart{}).Where("intent_id = ? AND ordinal = ?", corruptHeader.ID, 0).Update("data", []byte("retained corruption probe")).Error; err != nil {
		t.Fatal(err)
	}
	request(adminToken, corruptHeader.ID, string(raw), http.StatusAccepted)
	if done, err := (observationstore.Worker{DB: h.db}).Drain(t.Context(), 32, 8*1024*1024); err != nil || done != 1 {
		t.Fatalf("corrupt material disposition %d %v", done, err)
	}
	var corruptResult model.NodeObservationIntent
	if err := h.db.First(&corruptResult, "id = ?", corruptHeader.ID).Error; err != nil || corruptResult.Status != "held" || corruptResult.Reason != "invalid_material" || corruptResult.Attempts != 2 {
		t.Fatalf("corrupt material falsely applied %+v %v", corruptResult, err)
	}
	if err := h.db.First(&corruptNode, corruptNode.ID).Error; err != nil || corruptNode.ActiveFlows != 0 {
		t.Fatalf("corrupt material updated counters %+v %v", corruptNode, err)
	}
	var preserved model.NodeObservationPart
	if err := h.db.First(&preserved, "intent_id = ? AND ordinal = ?", corruptHeader.ID, 0).Error; err != nil || string(preserved.Data) != "retained corruption probe" {
		t.Fatalf("corrupt material replaced or discarded %v", err)
	}
	assertObservationAttemptLimit(t, h)
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	request(adminToken, header.ID, string(raw), http.StatusForbidden)
	if err := h.db.Model(&model.AuditLog{}).Where("action = ?", "node.observation.retry").Count(&auditCount).Error; err != nil || auditCount != 2 {
		t.Fatalf("unsafe retry wrote audit %d %v", auditCount, err)
	}
	var count int64
	if err := h.db.Model(&model.TrafficRecord{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("observation retry modified billing %d %v", count, err)
	}
}

func assertObservationAttemptLimit(t *testing.T, h *handlers) {
	t.Helper()
	node := model.Node{Name: "attempt-limit-original", Config: "{}", IsEnabled: true}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ids := []string{}
	for i := uint64(1); i <= 2; i++ {
		p := network.EventPosition{AssetTag: node.AssetTag, ID: "attempt-limit-" + strconv.FormatUint(i, 10), CoreInstanceID: "attempt-limit-core", Sequence: i, OccurredAt: now, ReceivedAt: now}
		source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p, StatsEvent: &p, Stats: network.NodeStats{ActiveSessions: i}}, Kind: "stats.sampled"}
		var id string
		if err := h.db.Transaction(func(tx *gorm.DB) error { var e error; id, e = observationstore.Enqueue(tx, source); return e }); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	limit := uint64(math.MaxUint64)
	if datastore.IsSQLite(h.db) {
		limit = math.MaxInt64
	}
	if err := h.db.Model(&model.NodeObservationIntent{}).Where("id = ?", ids[0]).Updates(map[string]any{"attempts": limit, "available_at": now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	done, err := (observationstore.Worker{DB: h.db}).Drain(t.Context(), 2, 8*1024*1024)
	if err != nil || done != 2 {
		t.Fatalf("terminal attempt blocked bounded round %d %v", done, err)
	}
	var exhausted, next model.NodeObservationIntent
	if err := h.db.First(&exhausted, "id = ?", ids[0]).Error; err != nil || exhausted.Status != "held" || exhausted.Reason != "attempt_limit" || exhausted.Attempts != limit || exhausted.Token != "" {
		t.Fatalf("attempt generation wrapped %+v %v", exhausted, err)
	}
	if err := h.db.First(&next, "id = ?", ids[1]).Error; err != nil || next.Status != "applied" || next.Attempts != 1 {
		t.Fatalf("next candidate not processed %+v %v", next, err)
	}
	var parts int64
	if err := h.db.Model(&model.NodeObservationPart{}).Where("intent_id = ?", ids[0]).Count(&parts).Error; err != nil || parts == 0 {
		t.Fatalf("terminal attempt lost material %d %v", parts, err)
	}
}
