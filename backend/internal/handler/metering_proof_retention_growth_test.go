package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
)

func TestSQLiteNativeProofFiniteGrowthObservationRetentionAndReplay(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertNativeProofRetentionGrowth(t, h, c)
}
func TestMySQLNativeProofFiniteGrowthObservationRetentionAndReplay(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertNativeProofRetentionGrowth(t, h, c)
}

type proofRetentionCheckpoint struct {
	proofGrowthSnapshot
	ObservationStatuses  map[string]int64 `json:"observation_status_rows"`
	RetiredHeaders       int64            `json:"retired_observation_headers"`
	RetiredOriginalBytes int64            `json:"retired_original_material_bytes"`
}

func assertNativeProofRetentionGrowth(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	const windows, samples = 16, 64
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	c.CredentialID = strings.Repeat("p", 96)
	if err := h.db.Model(&model.ProtocolCredential{}).Where("id = ?", c.ID).Update("credential_id", c.CredentialID).Error; err != nil {
		t.Fatal(err)
	}
	var node model.Node
	if err := h.db.First(&node, c.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if len(node.AssetTag) != 64 {
		t.Fatal("fixture has no authenticated asset admission label")
	}
	core := strings.Repeat("c", 190)
	// Capture an existing node label once, exactly as the authenticated ingress
	// stamps admitted envelopes. Never repair/relabel unknown historical input.
	stats := repeatedFlowEvents(c, 1)[0]
	stats.ID, stats.Type, stats.FlowID = "finite-retention-stats", "stats.sampled", ""
	stats.AssetTag, stats.SourceID, stats.CoreInstanceID = node.AssetTag, strings.Repeat("s", 190), core
	stats.Sequence = 12345678901234560
	stats.OccurredAt = stats.OccurredAt.Add(-time.Second)
	stats.Payload = json.RawMessage(`{"active_sessions":7,"bytes_up":13,"bytes_down":17}`)
	if err := h.projectZeroNodeEvents(t.Context(), []zeroevent.Envelope{stats}); err != nil {
		t.Fatal(err)
	}
	var all [][]zeroevent.Envelope
	var originals []model.TrafficRecord
	now := time.Now().UTC().Truncate(time.Millisecond).Add(8 * 24 * time.Hour)
	archive := meteringstore.ProofArchives{DB: h.db, Now: func() time.Time { return now }}
	var archiveCursor uint
	var checkpoints []proofRetentionCheckpoint
	capture := func(phase string) {
		checkpoints = append(checkpoints, captureProofRetentionGrowth(t, h.db, phase, windows))
	}
	for window := 0; window < windows; window++ {
		events := repeatedFlowEvents(c, samples)
		for i := range events {
			n := window*samples + i + 1
			events[i].ID = fmt.Sprintf("finite-retention-%02d-%03d", window, i)
			events[i].AssetTag, events[i].SourceID, events[i].CoreInstanceID = node.AssetTag, strings.Repeat("s", 190), core
			events[i].Sequence = stats.Sequence + uint64(n)
			events[i].OccurredAt = events[i].OccurredAt.Add(time.Duration(window) * time.Minute)
			events[i].Payload = json.RawMessage(fmt.Sprintf(`{"flow_id":"flood-flow","traffic":{"bytes_up":%d,"bytes_down":%d}}`, n, 2*n))
		}
		all = append(all, events)
		if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
			t.Fatal("native window", window, err)
		}
		var record model.TrafficRecord
		if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id DESC").First(&record).Error; err != nil {
			t.Fatal(err)
		}
		if record.RawBytes != samples*3 || record.UsedBytes != samples*5 || record.ProtocolMultiplierMilli != 1500 {
			t.Fatal("rounding changed before retention", record)
		}
		originals = append(originals, record)
		if (window+1)%4 != 0 {
			continue
		}
		if n, err := (meteringstore.UsageProjectionWorker{DB: h.db}).Drain(t.Context(), 200); err != nil || n != 4 {
			t.Fatal("actual usage projection", n, err)
		}
		for calls := 0; archiveCursor < record.ID; calls++ {
			if calls > 4 {
				t.Fatal("archive cursor did not advance")
			}
			out, err := archive.Archive(t.Context(), metering.ProofArchiveRequest{AfterID: archiveCursor, ThroughID: record.ID, Before: now.Add(-7 * 24 * time.Hour), Limit: 3})
			if err != nil || out.NextAfterID <= archiveCursor || out.Held != out.Scanned || out.Archived != 0 || out.NoSaving != 0 {
				t.Fatal("native proof archive", out, err)
			}
			archiveCursor = out.NextAfterID
		}
	}
	// Unknown auxiliary source retains its original identity/reason, but its
	// unreferenced body exits the explicit seven-day replay window with a gap.
	held := stats
	held.ID, held.Type, held.AssetTag = "finite-retention-unknown-asset", "engine.warning", ""
	held.Sequence = stats.Sequence + windows*samples + 1
	held.Payload = json.RawMessage(`{"message":"unknown original asset remains evidence"}`)
	if err := h.projectZeroNodeEvents(t.Context(), []zeroevent.Envelope{held}); err != nil {
		t.Fatal(err)
	}
	capture("before_observation_projection")
	worker := observationstore.Worker{DB: h.db}
	drainProofObservations(t, worker, windows*samples+2)
	capture("after_observation_projection_before_retention")
	var state model.NodeObservationState
	if err := h.db.First(&state, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil {
		t.Fatal(err)
	}
	if !state.LatestKnown || !state.StatsKnown || !state.CurrentKnown || state.CurrentSource != "buffered_stats" || state.LatestReference == "" || state.StatsReference == "" || state.LatestReference == state.StatsReference {
		t.Fatal("actual latest/stats pins not established", state)
	}
	var headersBefore []model.NodeObservationIntent
	if err := h.db.Order("id").Find(&headersBefore).Error; err != nil {
		t.Fatal(err)
	}
	retention := observationstore.MaterialRetention{DB: h.db, Now: func() time.Time { return now }}
	retired := drainProofMaterialRetention(t, retention)
	if retired != windows*samples {
		t.Fatal("unreferenced applied parts and expired unknown auxiliary source may retire", retired)
	}
	capture("after_seven_day_retention")
	last := checkpoints[len(checkpoints)-1]
	if last.Rows["node_observation_intents"] != windows*samples+2 || last.Rows["node_observation_parts"] != 2 || last.ObservationStatuses["held"] != 0 || last.ObservationStatuses["retired"] != 1 || last.RetiredHeaders != windows*samples {
		t.Fatal("necessary pins/held/header growth", last)
	}
	assertProofRetainedPins(t, h.db, state)
	assertProofHeaderIdentity(t, h.db, headersBefore)
	var cursorBefore []model.FlowUsage
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id").Find(&cursorBefore).Error; err != nil {
		t.Fatal(err)
	}
	for window := windows - 1; window >= 0; window-- {
		spool := &checkpointTestSpool{events: all[window]}
		if window == windows-1 {
			spool.commitErr = errors.New("retired proof checkpoint failure")
			if _, err := h.consumeZeroEventBatch(t.Context(), spool, samples); err == nil || spool.position != 0 {
				t.Fatal("failed replay checkpoint", spool.position, err)
			}
			spool.commitErr = nil
		}
		if n, err := h.consumeZeroEventBatch(t.Context(), spool, samples); err != nil || n != samples || spool.position != samples {
			t.Fatal("archived/retired bounded replay", window, n, err)
		}
	}
	capture("after_reverse_checkpoint_replay")
	afterReplay := checkpoints[len(checkpoints)-1]
	if afterReplay.Rows["node_observation_intents"] != last.Rows["node_observation_intents"] || afterReplay.Rows["node_observation_parts"] != last.Rows["node_observation_parts"] || afterReplay.ObservationPartBytes != last.ObservationPartBytes || afterReplay.RetiredHeaders != last.RetiredHeaders {
		t.Fatal("exact source replay rebuilt retired material", last, afterReplay)
	}
	if pending, err := worker.HasPending(t.Context()); err != nil || pending {
		t.Fatal("replay reset applied/retired receipt", pending, err)
	}
	late := all[0][0]
	late.ID = "finite-retention-unseen-late"
	if err := h.projectZeroNodeEvents(t.Context(), []zeroevent.Envelope{late}); err != nil {
		t.Fatal("unseen old sequence", err)
	}
	drainProofObservations(t, worker, 1)
	if retired := drainProofMaterialRetention(t, retention); retired != 1 {
		t.Fatal("unreferenced new late material did not retire independently", retired)
	}
	capture("after_unseen_late_apply_and_retire")
	var stateAfter model.NodeObservationState
	if err := h.db.First(&stateAfter, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil || !reflect.DeepEqual(state, stateAfter) {
		t.Fatal("retention/replay changed observation truth", err, state, stateAfter)
	}
	var cursorAfter []model.FlowUsage
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id").Find(&cursorAfter).Error; err != nil || !reflect.DeepEqual(cursorBefore, cursorAfter) {
		t.Fatal("retention/replay changed cumulative cursor", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, windows*samples*5, subStatusActive)
	page, err := (meteringstore.Records{DB: h.db}).Read(t.Context(), c.UserID, metering.RecordsQuery{Paged: true, Limit: 200, From: originals[0].At.Add(-time.Hour), To: originals[len(originals)-1].At.Add(time.Hour), SubscriptionID: c.SubscriptionID})
	if err != nil || len(page.Records) != windows {
		t.Fatal("post-retirement proof audit", len(page.Records), err)
	}
	byID := map[uint]model.TrafficRecord{}
	for _, item := range page.Records {
		byID[item.ID] = model.TrafficRecord(item)
	}
	for _, original := range originals {
		if restored, ok := byID[original.ID]; !ok || !reflect.DeepEqual(restored, original) {
			t.Fatal("financial evidence changed", original.ID)
		}
	}
	final := checkpoints[len(checkpoints)-1]
	if final.Rows["traffic_records"] != windows || final.Rows["metering_report_receipts"] != windows || final.Rows["traffic_record_meta_archives"] != 0 || final.Rows["metering_usage_projection_pending"] != 0 || final.Rows["node_observation_intents"] != windows*samples+3 || final.Rows["node_observation_parts"] != 2 {
		t.Fatal("final independent authority growth", final)
	}
	encoded, err := json.Marshal(struct {
		Driver      string                     `json:"driver"`
		Scope       string                     `json:"scope"`
		Checkpoints []proofRetentionCheckpoint `json:"checkpoints"`
	}{h.db.Dialector.Name(), "Finite synthetic windows; actual asset admission, projection, archive and retention. Necessary financial receipts and observation headers grow per accepted identity. Two current references remain; unknown auxiliary material has an explicit expired/gap receipt; no capacity/constant-total-size/physical-shrink claim. Body bytes are named variable material only; MySQL physical table values are estimates.", checkpoints})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("METERING_FINITE_RETENTION=" + string(encoded))
}

func drainProofObservations(t *testing.T, worker observationstore.Worker, want int) {
	t.Helper()
	done := 0
	for calls := 0; calls <= want/32+2; calls++ {
		n, err := worker.Drain(t.Context(), 32, observationstore.MaxDrainBytes)
		if err != nil {
			t.Fatal("bounded observation worker", n, err)
		}
		done += n
		if n == 0 {
			if done != want {
				t.Fatal("observation disposition count", done, want)
			}
			return
		}
	}
	t.Fatal("observation worker exceeded finite call budget", done, want)
}

func drainProofMaterialRetention(t *testing.T, retention observationstore.MaterialRetention) int {
	t.Helper()
	retired := 0
	originalNow := retention.Now
	elapsed := time.Duration(0)
	retention.Now = func() time.Time {
		if originalNow != nil {
			return originalNow().Add(elapsed)
		}
		return time.Now().UTC().Add(elapsed)
	}
	for calls := 0; calls < 40; calls++ {
		out, err := retention.Retire(t.Context(), 32, observationstore.MaxDrainBytes)
		if err != nil {
			t.Fatal("bounded observation retention", out, err)
		}
		retired += out.Retired
		elapsed += 2 * time.Second
		if out.Inspected == 0 {
			return retired
		}
	}
	t.Fatal("retention exceeded finite call budget")
	return retired
}

func captureProofRetentionGrowth(t *testing.T, db *gorm.DB, phase string, windows int) proofRetentionCheckpoint {
	t.Helper()
	out := proofRetentionCheckpoint{proofGrowthSnapshot: captureProofGrowth(t, db, phase, windows), ObservationStatuses: map[string]int64{}}
	var statuses []struct {
		Status string
		Count  int64
	}
	if err := db.Model(&model.NodeObservationIntent{}).Select("status, COUNT(*) AS count").Group("status").Scan(&statuses).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		out.ObservationStatuses[status.Status] = status.Count
	}
	if err := db.Model(&model.NodeObservationIntent{}).Where("material_retired_at IS NOT NULL").Count(&out.RetiredHeaders).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COALESCE(SUM(material_bytes),0) FROM node_observation_intents WHERE material_retired_at IS NOT NULL").Scan(&out.RetiredOriginalBytes).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

func assertProofRetainedPins(t *testing.T, db *gorm.DB, state model.NodeObservationState) {
	t.Helper()
	for _, id := range []string{state.LatestReference, state.StatsReference} {
		var header model.NodeObservationIntent
		if err := db.First(&header, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		var parts int64
		if err := db.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&parts).Error; err != nil || parts != int64(header.PartCount) || header.MaterialRetiredAt != nil || header.Status != "applied" {
			t.Fatal("current source pin lost material", header, parts, err)
		}
	}
	var held []model.NodeObservationIntent
	if err := db.Where("status = ? AND reason = ?", "retired", "unknown_source").Find(&held).Error; err != nil || len(held) != 1 {
		t.Fatal("unknown source lost compact retired gap", len(held), err)
	}
	var parts int64
	if err := db.Model(&model.NodeObservationPart{}).Where("intent_id = ?", held[0].ID).Count(&parts).Error; err != nil || parts != 0 || held[0].MaterialRetiredAt == nil || held[0].Reason != "unknown_source" {
		t.Fatal("expired unknown source lost identity or rebuilt body", parts, err)
	}
}

func assertProofHeaderIdentity(t *testing.T, db *gorm.DB, originals []model.NodeObservationIntent) {
	t.Helper()
	var after []model.NodeObservationIntent
	if err := db.Order("id").Find(&after).Error; err != nil || len(after) != len(originals) {
		t.Fatal("retention changed header inventory", len(after), err)
	}
	for i := range after {
		original := originals[i]
		original.MaterialReviewAt, original.MaterialRetiredAt = after[i].MaterialReviewAt, after[i].MaterialRetiredAt
		if original.Status == "held" && original.Reason == "unknown_source" && after[i].Status == "retired" {
			original.Status = "retired"
		}
		if !reflect.DeepEqual(original, after[i]) {
			t.Fatal("retention altered source/disposition identity", original.ID)
		}
	}
}
