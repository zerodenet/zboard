package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteNativeProofArchiveKeepsRoundedSettlementAndBoundedReplay(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertNativeProofArchiveReplay(t, h, c)
}
func TestMySQLNativeProofArchiveKeepsRoundedSettlementAndBoundedReplay(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertNativeProofArchiveReplay(t, h, c)
}

func assertNativeProofArchiveReplay(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	// Legal long native identifiers provide compressible, genuinely generated
	// proof. Typical small UUID proof must remain inline if sealing grows it.
	c.CredentialID = strings.Repeat("p", 96)
	if err := h.db.Model(&model.ProtocolCredential{}).Where("id = ?", c.ID).Update("credential_id", c.CredentialID).Error; err != nil {
		t.Fatal(err)
	}
	events := repeatedFlowEvents(c, 3)
	for i := range events {
		events[i].SourceID = strings.Repeat("s", 190)
		events[i].CoreInstanceID = strings.Repeat("c", 190)
		events[i].Sequence = 12345678901234560 + uint64(i)
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal("native accounting", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 15, subStatusActive)
	var original model.TrafficRecord
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).First(&original).Error; err != nil {
		t.Fatal(err)
	}
	if original.RawBytes != 9 || original.UsedBytes != 15 {
		t.Fatal("native per-sample rounding fixture changed", original)
	}
	// Explicit pre-0078 fixture: upgrades must still read and safely restore
	// sealed legacy event proofs. New native writers produce minute arithmetic
	// proofs and never create this event-body layout.
	original.IsAggregate, original.RecordCount = false, 1
	original.EventType, original.FlowID = "flow.updated", events[0].FlowID
	original.ReportID = events[len(events)-1].ID
	original.Nonce = meteringstoreReceiptNonce(original.ReportID)
	legacyProof, err := json.Marshal(map[string]any{"source_id": events[0].SourceID, "core_instance_id": events[0].CoreInstanceID, "sequence": events[len(events)-1].Sequence, "credential_id": c.CredentialID, "buffered": true, "accounting_samples": 3, "first_sequence": events[0].Sequence, "rounding_numerator": 1500})
	if err != nil {
		t.Fatal(err)
	}
	original.Meta = string(legacyProof)
	if err := h.db.Model(&model.TrafficRecord{}).Where("id = ?", original.ID).UpdateColumns(map[string]any{"is_aggregate": false, "record_count": 1, "event_type": original.EventType, "flow_id": original.FlowID, "report_id": original.ReportID, "nonce": original.Nonce, "meta": original.Meta, "updated_at": original.UpdatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	// The same pending charge now represents one legacy ledger record.
	if err := h.db.Model(&model.MeteringUsageProjectionPending{}).Where("traffic_record_id = ?", original.ID).UpdateColumns(map[string]any{"delta_raw_bytes": 0, "delta_upload_bytes": 0, "delta_download_bytes": 0, "delta_used_bytes": 0, "delta_record_count": 0}).Error; err != nil {
		t.Fatal(err)
	}
	var cursors []model.FlowUsage
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id").Find(&cursors).Error; err != nil {
		t.Fatal(err)
	}
	// The service clock and persisted chronology have millisecond precision.
	// Use that same clock for an exact seven-day boundary rather than making
	// the requested cutoff a fraction of a millisecond younger than permitted.
	now := time.Now().UTC().Truncate(time.Millisecond).Add(8 * 24 * time.Hour)
	archive := meteringstore.ProofArchives{DB: h.db, Now: func() time.Time { return now }}
	q := metering.ProofArchiveRequest{AfterID: original.ID - 1, ThroughID: original.ID, Before: now.Add(-7 * 24 * time.Hour), Limit: 32}
	// An outstanding projection pins its exact source. No marker or compressed
	// authority may be committed before the original checkpoint is consumed.
	if out, err := archive.Archive(t.Context(), q); err != nil || out.Archived != 0 || out.Held != 1 {
		t.Fatal("pending source was archived", out, err)
	}
	if n, err := (meteringstore.UsageProjectionWorker{DB: h.db}).Drain(t.Context(), 200); err != nil || n != 1 {
		t.Fatal("actual projection", n, err)
	}
	checkpointFailure := errors.New("injected archive marker checkpoint failure")
	const callback = "test:native-proof-marker-failure"
	if err := h.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "traffic_records" {
			tx.AddError(checkpointFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	out, err := archive.Archive(t.Context(), q)
	if !errors.Is(err, checkpointFailure) || out != (metering.ProofArchiveResult{}) {
		t.Fatal("unconfirmed archive advanced its receipt", out, err)
	}
	if err := h.db.Callback().Update().Remove(callback); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := h.db.Model(&model.TrafficMetaArchive{}).Where("traffic_record_id = ?", original.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("rolled-back payload survived", count, err)
	}
	out, err = archive.Archive(t.Context(), q)
	if err != nil || out.Archived != 1 || out.RawBytes <= out.EstimatedStoredBytes+64 || out.NextAfterID != original.ID {
		t.Fatal("native proof not safely sealed", out, len(original.Meta), err)
	}
	var sealed model.TrafficRecord
	if err := h.db.First(&sealed, original.ID).Error; err != nil {
		t.Fatal(err)
	}
	compare := sealed
	compare.Meta = original.Meta
	if !reflect.DeepEqual(compare, original) || sealed.Meta == original.Meta || sealed.Nonce != original.Nonce || sealed.EntitlementEpoch != original.EntitlementEpoch {
		before, after := reflect.ValueOf(original), reflect.ValueOf(compare)
		for i := 0; i < before.NumField(); i++ {
			if !reflect.DeepEqual(before.Field(i).Interface(), after.Field(i).Interface()) {
				t.Logf("source field %s changed: %v -> %v", before.Type().Field(i).Name, before.Field(i).Interface(), after.Field(i).Interface())
			}
		}
		t.Fatal("archive rewrote billing identity or chronology")
	}
	if repeat, err := archive.Archive(t.Context(), q); err != nil || repeat.AlreadySealed != 1 || repeat.Archived != 0 {
		t.Fatal("archive replay duplicated payload", repeat, err)
	}
	if err := datastore.PrepareHourlyProjectionRollback(h.db); err == nil || !strings.Contains(err.Error(), "restore all native proof") {
		t.Fatal("older binary admitted without restoring live proof", err)
	}
	query := metering.RecordsQuery{Paged: true, Limit: 20, From: original.At.Add(-time.Hour), To: original.At.Add(time.Hour), SubscriptionID: c.SubscriptionID}
	page, err := (meteringstore.Records{DB: h.db}).Read(t.Context(), c.UserID, query)
	if err != nil || len(page.Records) != 1 || page.Records[0].Meta != original.Meta {
		t.Fatal("full authorized read lost native proof", err)
	}
	var header model.TrafficMetaArchive
	if err := h.db.First(&header, original.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, damaged := range []string{strings.Replace(sealed.Meta, "proof_archive", "proof_archivf", 1), strings.Replace(sealed.Meta, `"type":`, `"typx":`, 1)} {
		if err := h.db.Model(&model.TrafficRecord{}).Where("id = ?", original.ID).UpdateColumns(map[string]any{"meta": damaged, "updated_at": original.UpdatedAt}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := (meteringstore.Records{DB: h.db}).Read(t.Context(), c.UserID, query); !errors.Is(err, metering.ErrProofArchive) {
			t.Fatal("damaged discriminator bypassed archive authority", err)
		}
		if result, err := archive.Archive(t.Context(), q); err == nil || result != (metering.ProofArchiveResult{}) {
			t.Fatal("damaged discriminator accepted as held unknown raw proof", result, err)
		}
		if err := datastore.PrepareHourlyProjectionRollback(h.db); err == nil || !strings.Contains(err.Error(), "native proof") {
			t.Fatal("damaged discriminator bypassed downgrade gate", err)
		}
	}
	if err := h.db.Model(&model.TrafficRecord{}).Where("id = ?", original.ID).UpdateColumns(map[string]any{"meta": sealed.Meta, "updated_at": original.UpdatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	legacy := original
	legacy.ID = 0
	legacy.EventType = ""
	legacy.FlowID = ""
	legacy.ReportID = "legacy-archive-type-diagnostic"
	legacy.Nonce = "legacy-archive-type-nonce"
	legacy.RawBytes = 0
	legacy.UploadBytes = 0
	legacy.DownloadBytes = 0
	legacy.UsedBytes = 0
	legacy.Meta = sealed.Meta
	if err := h.db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	legacyPage, err := (meteringstore.Records{DB: h.db}).Read(t.Context(), c.UserID, query)
	if err != nil {
		t.Fatal("legacy diagnostic type was interpreted as archive authority", err)
	}
	foundLegacy := false
	for _, r := range legacyPage.Records {
		if r.ID == legacy.ID {
			foundLegacy = r.Meta == legacy.Meta
		}
	}
	if !foundLegacy {
		t.Fatal("legacy same-name diagnostic changed")
	}
	legacyRange := q
	legacyRange.AfterID = legacy.ID - 1
	legacyRange.ThroughID = legacy.ID
	if result, err := archive.Archive(t.Context(), legacyRange); err != nil || result.Held != 1 || result.Archived != 0 || result.AlreadySealed != 0 {
		t.Fatal("legacy diagnostic mislabeled internal archive", result, err)
	}
	saved := append([]byte{}, header.Payload...)
	broken := append([]byte{}, saved...)
	broken[len(broken)/2] ^= 1
	if err := h.db.Model(&model.TrafficMetaArchive{}).Where("traffic_record_id = ?", original.ID).Update("payload", broken).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (meteringstore.Records{DB: h.db}).Read(t.Context(), c.UserID, query); err == nil {
		t.Fatal("corrupt proof silently reported complete")
	}
	if restored, err := archive.Restore(t.Context(), original.ID); err == nil || restored != (metering.ProofRestoreResult{}) {
		t.Fatal("corrupt proof restored", restored, err)
	}
	// The core facts and independent summaries remain usable even if an audit
	// archive is damaged; they do not decode proof material to settle quota.
	if _, err := (meteringstore.Records{DB: h.db}).ReadSummaryPage(t.Context(), c.UserID, query); err != nil {
		t.Fatal("summary depended on archive material", err)
	}
	spool := &checkpointTestSpool{events: events, commitErr: errors.New("archive replay checkpoint failure")}
	if _, err := h.consumeZeroEventBatch(t.Context(), spool, 2000); err == nil || spool.position != 0 {
		t.Fatal("failed checkpoint acknowledged progress", spool.position, err)
	}
	spool.commitErr = nil
	if n, err := h.consumeZeroEventBatch(t.Context(), spool, 2000); err != nil || n != len(events) || spool.position != len(events) {
		t.Fatal("archived native replay", n, spool.position, err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 15, subStatusActive)
	var afterCursors []model.FlowUsage
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id").Find(&afterCursors).Error; err != nil || !reflect.DeepEqual(cursors, afterCursors) {
		t.Fatal("archived replay changed cumulative cursors", err)
	}
	if err := h.db.Model(&model.TrafficMetaArchive{}).Where("traffic_record_id = ?", original.ID).Update("payload", saved).Error; err != nil {
		t.Fatal(err)
	}
	if result, err := archive.Restore(t.Context(), original.ID); err != nil || !result.Restored {
		t.Fatal("verified proof restoration", result, err)
	}
	if result, err := archive.Restore(t.Context(), original.ID); err != nil || result.Restored {
		t.Fatal("restoration is not idempotent", result, err)
	}
	var restored model.TrafficRecord
	if err := h.db.First(&restored, original.ID).Error; err != nil || !reflect.DeepEqual(original, restored) {
		t.Fatal("restoration changed original source", err)
	}
	var retained model.TrafficMetaArchive
	if err := h.db.First(&retained, original.ID).Error; err != nil || !bytes.Equal(saved, retained.Payload) {
		t.Fatal("restoration deleted immutable archive evidence", err)
	}
	// A fully restored inline proof is verified from its own exact byte/hash
	// relationship and does not decode a retained copy merely to read history.
	if err := h.db.Model(&model.TrafficMetaArchive{}).Where("traffic_record_id = ?", original.ID).Update("payload", broken).Error; err != nil {
		t.Fatal(err)
	}
	if page, err := (meteringstore.Records{DB: h.db}).Read(t.Context(), c.UserID, query); err != nil || len(page.Records) != 2 {
		t.Fatal("restored raw proof unnecessarily depended on compressed copy", err)
	}
}
