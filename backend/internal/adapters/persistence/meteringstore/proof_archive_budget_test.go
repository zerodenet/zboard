package meteringstore

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestSQLiteNativeProofArchiveByteBudgetKeepsUnknownEvidenceAndCursor(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "archive-budget.db"))
	if err := db.Exec("CREATE TABLE schema_migrations(version text PRIMARY KEY,applied_at datetime NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	ddl, err := migrations.Files.ReadFile("sqlite/0064_traffic_record_meta_archives.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(ddl)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO schema_migrations VALUES (?,?)", "0064_traffic_record_meta_archives.up.sql", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Millisecond)
	rows := make([]model.TrafficRecord, 20)
	for i := range rows {
		rows[i] = model.TrafficRecord{NodeID: 1, ReportID: fmt.Sprintf("unknown-%02d", i), Nonce: fmt.Sprintf("nonce-%02d", i), RawBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: old, CreatedAt: old, UpdatedAt: old, Meta: strings.Repeat("x", proofArchiveBodyLimit)}
	}
	if err := db.CreateInBatches(&rows, 5).Error; err != nil {
		t.Fatal(err)
	}
	var original []model.TrafficRecord
	if err := db.Order("id").Find(&original).Error; err != nil {
		t.Fatal(err)
	}
	store := ProofArchives{DB: db}
	q := metering.ProofArchiveRequest{ThroughID: rows[len(rows)-1].ID, Before: old.Add(time.Hour), Limit: 32}
	out, err := store.Archive(t.Context(), q)
	if err != nil || out.Scanned != 16 || out.Held != 16 || out.Archived != 0 || out.NextAfterID != rows[15].ID || out.RangeExhausted {
		t.Fatal("byte bound lost its incomplete range", out, err)
	}
	q.AfterID = out.NextAfterID
	out, err = store.Archive(t.Context(), q)
	if err != nil || out.Scanned != 4 || out.Held != 4 || out.Archived != 0 || !out.RangeExhausted {
		t.Fatal("bounded continuation skipped evidence", out, err)
	}
	for _, bad := range []metering.ProofArchiveRequest{{ThroughID: 0, Before: q.Before, Limit: 32}, {ThroughID: q.ThroughID, Before: q.Before, Limit: 33}, {ThroughID: q.ThroughID, Before: time.Now().Add(time.Hour), Limit: 32}} {
		if _, err := store.Archive(t.Context(), bad); err == nil {
			t.Fatal("unbounded or future maintenance admitted", bad)
		}
	}
	var after []model.TrafficRecord
	if err := db.Order("id").Find(&after).Error; err != nil || !reflect.DeepEqual(original, after) {
		t.Fatal("unknown evidence or billing changed", err)
	}
	var count int64
	if err := db.Model(&model.TrafficMetaArchive{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("unknown history was claimed as archived", count, err)
	}
}

func TestSQLiteNativeProofArchiveBudgetIncludesAlreadySealedDecompression(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "archive-sealed-budget.db"))
	if err := db.Exec("CREATE TABLE schema_migrations(version text PRIMARY KEY,applied_at datetime NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	ddl, err := migrations.Files.ReadFile("sqlite/0064_traffic_record_meta_archives.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(ddl)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO schema_migrations VALUES (?,?)", "0064_traffic_record_meta_archives.up.sql", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Millisecond)
	proof := `{"source_id":"source","core_instance_id":"runtime","sequence":1,"credential_id":"credential","buffered":true,"accounting_samples":1,"first_sequence":1,"rounding_numerator":500}`
	proof += strings.Repeat(" ", proofArchiveBodyLimit-len(proof))
	packed, err := compressProof(proof)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]model.TrafficRecord, 20)
	for i := range rows {
		report := fmt.Sprintf("sealed-%02d", i)
		rows[i] = model.TrafficRecord{NodeID: 1, UserID: 1, SubscriptionID: 1, ProtocolEndpointID: 1, ReportID: report, Nonce: completionNonce(report), FlowID: "flow", EventType: "flow.updated", RawBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: old, CreatedAt: old, UpdatedAt: old, Meta: archiveReference(proofBodyDigest(proof))}
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
		header := model.TrafficMetaArchive{TrafficRecordID: rows[i].ID, Version: 1, SourceBytes: uint64(len(proof)), SourceSHA256: proofBodyDigest(proof), FactSHA256: proofFactDigest(rows[i]), CompressedBytes: uint64(len(packed)), Payload: packed, CreatedAt: old}
		if err := db.Create(&header).Error; err != nil {
			t.Fatal(err)
		}
	}
	q := metering.ProofArchiveRequest{ThroughID: rows[len(rows)-1].ID, Before: old.Add(time.Hour), Limit: 32}
	out, err := (ProofArchives{DB: db}).Archive(t.Context(), q)
	if err != nil || out.AlreadySealed == 0 || out.AlreadySealed >= len(rows) || out.Scanned != out.AlreadySealed || out.RangeExhausted || out.NextAfterID != rows[out.AlreadySealed-1].ID {
		t.Fatal("small markers hid unbounded decompression", out, err)
	}
	q.AfterID = out.NextAfterID
	remaining, err := (ProofArchives{DB: db}).Archive(t.Context(), q)
	if err != nil || out.AlreadySealed+remaining.AlreadySealed != len(rows) || !remaining.RangeExhausted {
		t.Fatal("sealed bounded replay lost remaining rows", remaining, err)
	}
}

func TestSQLiteNativeProofArchiveFullReadsRejectRowsAndOutputBeforeLoading(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "archive-full-read-budget.db"))
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{ID: 1, Email: "proof-read@example.test", Password: "unused", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE schema_migrations(version text PRIMARY KEY,applied_at datetime NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	ddl, err := migrations.Files.ReadFile("sqlite/0064_traffic_record_meta_archives.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(ddl)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO schema_migrations VALUES (?,?)", "0064_traffic_record_meta_archives.up.sql", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Millisecond)
	proof := `{"source_id":"source","core_instance_id":"runtime","sequence":1,"credential_id":"credential","buffered":true,"accounting_samples":1,"first_sequence":1,"rounding_numerator":500}`
	proof += strings.Repeat(" ", proofArchiveBodyLimit-len(proof))
	packed, err := compressProof(proof)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 150; i++ {
		report := fmt.Sprintf("full-sealed-%03d", i)
		r := model.TrafficRecord{NodeID: 1, UserID: 1, SubscriptionID: 1, ProtocolEndpointID: 1, ReportID: report, Nonce: completionNonce(report), FlowID: "flow", EventType: "flow.updated", RawBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: old, CreatedAt: old, UpdatedAt: old, Meta: archiveReference(proofBodyDigest(proof))}
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		h := model.TrafficMetaArchive{TrafficRecordID: r.ID, Version: 1, SourceBytes: uint64(len(proof)), SourceSHA256: proofBodyDigest(proof), FactSHA256: proofFactDigest(r), CompressedBytes: uint64(len(packed)), Payload: packed, CreatedAt: old}
		if err := db.Create(&h).Error; err != nil {
			t.Fatal(err)
		}
	}
	loads, decodes := 0, 0
	const name = "test:full-proof-read-preflight"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]model.TrafficRecord); ok {
			loads++
		}
		if tx.Statement.Table == "traffic_record_meta_archives" && strings.Contains(strings.Join(tx.Statement.Selects, " "), "substr(payload") {
			decodes++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(name) })
	read := Records{DB: db}
	q := metering.RecordsQuery{Paged: true, Limit: 200, From: old.Add(-time.Hour), To: old.Add(time.Hour)}
	if _, err := read.Read(t.Context(), 1, q); !errors.Is(err, metering.ErrProofReadBudget) {
		t.Fatal("paged decompression output was unbounded", err)
	}
	if loads != 0 || decodes != 0 {
		t.Fatal("budget checked after allocating full source or decompressing", loads, decodes)
	}
	if page, err := read.ReadSummaryPage(t.Context(), 1, q); err != nil || len(page.Records) != 150 {
		t.Fatal("summary inherited full proof limits", err)
	}
	// Enough small marker/legacy rows also require a hard discovery row bound;
	// limiting decompression alone must not allow Find(all history) allocation.
	more := make([]model.TrafficRecord, fullProofReadRows+1-150)
	for i := range more {
		more[i] = model.TrafficRecord{NodeID: 1, UserID: 1, ReportID: fmt.Sprintf("small-%04d", i), Nonce: fmt.Sprintf("small-nonce-%04d", i), At: old, CreatedAt: old, UpdatedAt: old}
	}
	if err := db.CreateInBatches(&more, 100).Error; err != nil {
		t.Fatal(err)
	}
	loads, decodes = 0, 0
	if _, err := read.Read(t.Context(), 1, metering.RecordsQuery{}); !errors.Is(err, metering.ErrProofReadBudget) {
		t.Fatal("unpaged metadata heap was unbounded", err)
	}
	if loads != 0 || decodes != 0 {
		t.Fatal("unpaged bound enforced only after full loading", loads, decodes)
	}
	q.Limit = 10
	if page, err := read.Read(t.Context(), 1, q); err != nil || len(page.Records) != 10 {
		t.Fatal("ordinary paged history stopped working", err)
	}
}
