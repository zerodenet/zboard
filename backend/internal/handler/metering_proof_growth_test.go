package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
)

// This finite resource experiment is an actual collector/receipt/archive test,
// not a capacity threshold or an assertion that necessary facts stop growing.
func TestSQLiteNativeProofFiniteWindowGrowthAndReplay(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertNativeProofFiniteGrowth(t, h, c)
}

func TestMySQLNativeProofFiniteWindowGrowthAndReplay(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertNativeProofFiniteGrowth(t, h, c)
}

type proofGrowthSnapshot struct {
	Phase                string           `json:"phase"`
	Windows              int              `json:"windows"`
	Rows                 map[string]int64 `json:"rows"`
	LiveMetaBytes        int64            `json:"live_meta_bytes"`
	ArchivePayloadBytes  int64            `json:"archive_payload_bytes"`
	ArchiveOriginalBytes int64            `json:"archive_original_bytes"`
	ObservationPartBytes int64            `json:"observation_part_bytes"`
	RawBytes             int64            `json:"raw_bytes"`
	UsedBytes            int64            `json:"used_bytes"`
	SQLitePageBytes      int64            `json:"sqlite_allocated_page_bytes,omitempty"`
	SQLiteFreePageBytes  int64            `json:"sqlite_free_page_bytes,omitempty"`
	SQLiteDBFileBytes    int64            `json:"sqlite_database_file_bytes,omitempty"`
	SQLiteWALFileBytes   int64            `json:"sqlite_wal_file_bytes,omitempty"`
	MySQLDataEstimate    int64            `json:"mysql_table_data_bytes_estimate,omitempty"`
	MySQLIndexEstimate   int64            `json:"mysql_table_index_bytes_estimate,omitempty"`
}

func assertNativeProofFiniteGrowth(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	const windows, samples, archiveBatch = 16, 64, 3
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	c.CredentialID = strings.Repeat("p", 96)
	if err := h.db.Model(&model.ProtocolCredential{}).Where("id = ?", c.ID).Update("credential_id", c.CredentialID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond).Add(8 * 24 * time.Hour)
	archive := meteringstore.ProofArchives{DB: h.db, Now: func() time.Time { return now }}
	var snapshots []proofGrowthSnapshot
	snapshot := func(phase string, n int) { snapshots = append(snapshots, captureProofGrowth(t, h.db, phase, n)) }
	snapshot("baseline", 0)
	var cursor uint
	var all [][]zeroevent.Envelope
	var originals []model.TrafficRecord
	for window := 0; window < windows; window++ {
		events := repeatedFlowEvents(c, samples)
		for i := range events {
			n := window*samples + i + 1
			events[i].ID = fmt.Sprintf("finite-growth-%02d-%03d", window, i)
			events[i].SourceID = strings.Repeat("s", 190)
			events[i].CoreInstanceID = strings.Repeat("c", 190)
			events[i].Sequence = 12345678901234560 + uint64(n)
			events[i].OccurredAt = events[i].OccurredAt.Add(time.Duration(window) * time.Minute)
			events[i].Payload = json.RawMessage(fmt.Sprintf(`{"flow_id":"flood-flow","traffic":{"bytes_up":%d,"bytes_down":%d}}`, n, 2*n))
		}
		all = append(all, events)
		if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
			t.Fatal("native window", window, err)
		}
		assertAccountingTotal(t, h, c.SubscriptionID, int64((window+1)*samples*5), subStatusActive)
		var record model.TrafficRecord
		if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id DESC").First(&record).Error; err != nil {
			t.Fatal(err)
		}
		// Each report advances by 3 raw bytes. The authoritative writer must
		// preserve ceil(3*1.5)=5 per report rather than rounding the window once.
		if record.RawBytes != samples*3 || record.UsedBytes != samples*5 || record.ProtocolMultiplierMilli != 1500 {
			t.Fatal("window amount/multiplier changed", window, record)
		}
		var evidence struct {
			Samples  uint64 `json:"accounting_samples"`
			Rounding uint64 `json:"rounding_numerator"`
		}
		if err := json.Unmarshal([]byte(record.Meta), &evidence); err != nil || evidence.Samples != samples || evidence.Rounding != samples*500 {
			t.Fatal("native rounding proof", window, evidence, err)
		}
		originals = append(originals, record)
		if (window+1)%4 != 0 {
			continue
		}
		snapshot("before_archive", window+1)
		if n, err := (meteringstore.UsageProjectionWorker{DB: h.db}).Drain(t.Context(), 200); err != nil || n != 4 {
			t.Fatal("projection checkpoint", n, err)
		}
		through := record.ID
		sealed := 0
		for calls := 0; cursor < through; calls++ {
			if calls > 4 {
				t.Fatal("bounded archive did not advance", cursor, through)
			}
			result, err := archive.Archive(t.Context(), metering.ProofArchiveRequest{AfterID: cursor, ThroughID: through, Before: now.Add(-7 * 24 * time.Hour), Limit: archiveBatch})
			if err != nil || result.NextAfterID <= cursor || result.Held != 0 || result.NoSaving != 0 {
				t.Fatal("archive checkpoint", result, err)
			}
			sealed += result.Archived
			cursor = result.NextAfterID
		}
		if sealed != 4 {
			t.Fatal("archive failed to seal four real window proofs", sealed)
		}
		snapshot("after_archive", window+1)
		page, err := (meteringstore.Records{DB: h.db}).Read(t.Context(), c.UserID, metering.RecordsQuery{Paged: true, Limit: 200, From: originals[0].At.Add(-time.Hour), To: record.At.Add(time.Hour), SubscriptionID: c.SubscriptionID})
		if err != nil || len(page.Records) != window+1 {
			t.Fatal("bounded restored audit page", len(page.Records), err)
		}
		byID := make(map[uint]model.TrafficRecord, len(page.Records))
		for _, item := range page.Records {
			byID[item.ID] = model.TrafficRecord(item)
		}
		for _, original := range originals {
			restored, ok := byID[original.ID]
			if !ok || !reflect.DeepEqual(restored, original) {
				t.Fatal("archive changed original proof, identity, time or amounts", original.ID)
			}
		}
	}
	var before []model.FlowUsage
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatal("fixed one-flow resource fixture changed", len(before))
	}
	// Replay older windows in reverse arrival order. Each reader checkpoint is
	// bounded to 64 original reports; no unlimited in-memory history is replayed.
	for window := windows - 1; window >= 0; window-- {
		spool := &checkpointTestSpool{events: all[window]}
		if window == windows-1 {
			spool.commitErr = errors.New("finite replay checkpoint failure")
			if _, err := h.consumeZeroEventBatch(t.Context(), spool, samples); err == nil || spool.position != 0 {
				t.Fatal("failed durable checkpoint acknowledged", err, spool.position)
			}
			spool.commitErr = nil
		}
		if n, err := h.consumeZeroEventBatch(t.Context(), spool, samples); err != nil || n != samples || spool.position != samples {
			t.Fatal("bounded archived replay", window, n, err)
		}
	}
	// A previously unseen, late low sequence is a distinct report identity.
	// It must not be mistaken for fresh quota consumption after archival.
	late := all[0][0]
	late.ID = "finite-growth-unseen-late-report"
	if err := h.projectZeroNodeEvents(t.Context(), []zeroevent.Envelope{late}); err != nil {
		t.Fatal("late old cumulative snapshot", err)
	}
	var after []model.FlowUsage
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Order("id").Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("late/replay changed cumulative cursor", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, windows*samples*5, subStatusActive)
	snapshot("after_reverse_replay_and_unseen_late", windows)
	last := snapshots[len(snapshots)-1]
	if last.Rows["traffic_records"] != windows || last.Rows["metering_report_receipts"] != windows || last.Rows["traffic_record_meta_archives"] != windows || last.Rows["metering_usage_projection_pending"] != 0 || last.RawBytes != windows*samples*3 || last.UsedBytes != windows*samples*5 {
		t.Fatal("financial growth/replay boundary", last)
	}
	result := struct {
		Driver           string                `json:"driver"`
		Windows          int                   `json:"windows"`
		ReportsPerWindow int                   `json:"reports_per_window"`
		ArchiveBatch     int                   `json:"archive_batch_limit"`
		ReplayBatch      int                   `json:"replay_batch_limit"`
		Caveat           string                `json:"measurement_scope"`
		Snapshots        []proofGrowthSnapshot `json:"snapshots"`
	}{h.db.Dialector.Name(), windows, samples, archiveBatch, samples, "Finite synthetic minute windows; not elapsed long-duration/capacity acceptance. Ledger/receipt/archive authority grows linearly; no fact deletion. Byte fields cover named variable bodies, not total encoded row/index cost. SQLite pages/files cover whole isolated fixture including observation intents; MySQL information_schema bytes are estimates and can remain stale. No VACUUM, ANALYZE, TRUNCATE or physical shrink is requested.", snapshots}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("METERING_FINITE_GROWTH=" + string(encoded))
}

func captureProofGrowth(t *testing.T, db *gorm.DB, phase string, windows int) proofGrowthSnapshot {
	t.Helper()
	out := proofGrowthSnapshot{Phase: phase, Windows: windows, Rows: map[string]int64{}}
	for _, table := range []string{"traffic_records", "metering_report_receipts", "traffic_record_meta_archives", "flow_usages", "metering_usage_projection_pending", "node_observation_intents", "node_observation_parts"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil {
			t.Fatal(table, err)
		}
		out.Rows[table] = n
	}
	for _, item := range []struct {
		query  string
		target *int64
	}{
		{"SELECT COALESCE(SUM(octet_length(meta)),0) FROM traffic_records", &out.LiveMetaBytes},
		{"SELECT COALESCE(SUM(octet_length(payload)),0) FROM traffic_record_meta_archives", &out.ArchivePayloadBytes},
		{"SELECT COALESCE(SUM(source_bytes),0) FROM traffic_record_meta_archives", &out.ArchiveOriginalBytes},
		{"SELECT COALESCE(SUM(octet_length(data)),0) FROM node_observation_parts", &out.ObservationPartBytes},
		{"SELECT COALESCE(SUM(raw_bytes),0) FROM traffic_records", &out.RawBytes},
		{"SELECT COALESCE(SUM(used_bytes),0) FROM traffic_records", &out.UsedBytes},
	} {
		if err := db.Raw(item.query).Scan(item.target).Error; err != nil {
			t.Fatal(err)
		}
	}
	if db.Dialector.Name() == "sqlite" {
		var pages, pageSize, free int64
		for _, item := range []struct {
			query  string
			target *int64
		}{{"PRAGMA page_count", &pages}, {"PRAGMA page_size", &pageSize}, {"PRAGMA freelist_count", &free}} {
			if err := db.Raw(item.query).Scan(item.target).Error; err != nil {
				t.Fatal(err)
			}
		}
		out.SQLitePageBytes, out.SQLiteFreePageBytes = pages*pageSize, free*pageSize
		var files []struct {
			Name string
			File string
		}
		if err := db.Raw("PRAGMA database_list").Scan(&files).Error; err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if file.Name != "main" || file.File == "" {
				continue
			}
			info, err := os.Stat(file.File)
			if err != nil {
				t.Fatal(err)
			}
			out.SQLiteDBFileBytes = info.Size()
			if wal, err := os.Stat(file.File + "-wal"); err == nil {
				out.SQLiteWALFileBytes = wal.Size()
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		}
	} else {
		var estimate struct {
			DataBytes  int64
			IndexBytes int64
		}
		if err := db.Raw("SELECT COALESCE(SUM(data_length),0) AS data_bytes, COALESCE(SUM(index_length),0) AS index_bytes FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&estimate).Error; err != nil {
			t.Fatal(err)
		}
		out.MySQLDataEstimate, out.MySQLIndexEstimate = estimate.DataBytes, estimate.IndexBytes
	}
	return out
}
