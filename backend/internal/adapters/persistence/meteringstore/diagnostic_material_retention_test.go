package meteringstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func diagnosticMaterialDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "diagnostic-material.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db.Logger = logger.Discard
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func createDiagnosticSource(t *testing.T, db *gorm.DB, meta string, capture bool) model.TrafficRecord {
	t.Helper()
	id := uuid.NewString()
	record := model.TrafficRecord{UserID: 1, SubscriptionID: 1, NodeID: 1, ProtocolEndpointID: 1, ReportID: id, Nonce: id, RawBytes: 3, DownloadBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: time.Now().UTC(), Meta: boundedNodeReportMetadata(meta)}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if capture {
			return enqueueDiagnosticMaterial(tx, record, meta, time.Now())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return record
}

func loadDiagnosticSource(t *testing.T, db *gorm.DB, id uint) model.TrafficRecord {
	t.Helper()
	var row model.TrafficRecord
	if err := db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSQLiteDiagnosticMaterialRetiresOnlyProvenBodiesAndPreservesAllFacts(t *testing.T) {
	db := diagnosticMaterialDB(t)
	original := strings.Repeat("诊", 1000) + "\x00tail"
	captured := createDiagnosticSource(t, db, original, true)
	before := loadDiagnosticSource(t, db, captured.ID)
	unknown := createDiagnosticSource(t, db, strings.Repeat("legacy unclassified", 80), false)
	native := createDiagnosticSource(t, db, `{"rounding_numerator":1000,"accounting_samples":3}`, false)
	if err := db.Model(&model.TrafficRecord{}).Where("id = ?", native.ID).UpdateColumns(map[string]any{"event_type": "flow.updated", "flow_id": "native-flow"}).Error; err != nil {
		t.Fatal(err)
	}
	nativeBefore := loadDiagnosticSource(t, db, native.ID)
	omitted := createDiagnosticSource(t, db, strings.Repeat("m", nodeReportMetadataByteLimit+1), true)
	worker := DiagnosticMaterialRetention{DB: db}
	if ready, err := worker.HasPending(t.Context()); err != nil || ready {
		t.Fatal("young material eligible", err)
	}
	now := time.Now().UTC().Add(8 * 24 * time.Hour).Truncate(time.Millisecond)
	worker.Now = func() time.Time { return now }
	out, err := worker.Retire(t.Context(), 32, DiagnosticMaterialReadBudget)
	if err != nil || out.Retired != 1 || out.Bytes != uint64(len(original)) {
		t.Fatal("retirement", out, err)
	}
	after := loadDiagnosticSource(t, db, captured.ID)
	var receipt struct {
		Type          string
		Version       int
		OriginalBytes int `json:"original_bytes"`
		SHA256        string
		RetiredAt     time.Time `json:"retired_at"`
	}
	digest := sha256.Sum256([]byte(original))
	if err := json.Unmarshal([]byte(after.Meta), &receipt); err != nil || receipt.Type != "diagnostic_meta_retired" || receipt.Version != 1 || receipt.OriginalBytes != len(original) || receipt.SHA256 != hex.EncodeToString(digest[:]) || !receipt.RetiredAt.Equal(now) || len(after.Meta) > 512 {
		t.Fatal("retirement receipt invalid", err)
	}
	before.Meta = after.Meta
	if !reflect.DeepEqual(before, after) {
		t.Fatal("retirement changed financial/source/timestamp fields")
	}
	if loadDiagnosticSource(t, db, unknown.ID).Meta != unknown.Meta || !reflect.DeepEqual(loadDiagnosticSource(t, db, native.ID), nativeBefore) || loadDiagnosticSource(t, db, omitted.ID).Meta != omitted.Meta {
		t.Fatal("unclassified/native/compact material changed")
	}
	var header model.TrafficDiagnosticMaterial
	if err := db.First(&header, "traffic_record_id = ?", captured.ID).Error; err != nil || header.SourceBytes != uint64(len(original)) || header.SourceSHA256 != hex.EncodeToString(digest[:]) || header.RetiredAt == nil || !header.RetiredAt.Equal(now) {
		t.Fatal("original provenance lost", err)
	}
	if out, err := worker.Retire(t.Context(), 32, DiagnosticMaterialReadBudget); err != nil || out.Retired != 0 {
		t.Fatal("repeated retirement", out, err)
	}
	if ready, err := worker.HasPending(t.Context()); err != nil || ready {
		t.Fatal("retired source remained pending", err)
	}
	if err := db.Delete(&model.TrafficRecord{}, captured.ID).Error; err == nil {
		t.Fatal("provenance source foreign key no longer protects the financial fact")
	}
}

func TestSQLiteDiagnosticMaterialRollbackPreservesOriginalBodyAndReceipt(t *testing.T) {
	db := diagnosticMaterialDB(t)
	row := createDiagnosticSource(t, db, strings.Repeat("body", 1000), true)
	before := loadDiagnosticSource(t, db, row.ID)
	if err := db.Exec(`CREATE TRIGGER reject_diagnostic_retirement BEFORE UPDATE OF retired_at ON traffic_diagnostic_materials BEGIN SELECT RAISE(ABORT,'retirement unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	worker := DiagnosticMaterialRetention{DB: db, Now: func() time.Time { return time.Now().UTC().Add(8 * 24 * time.Hour) }}
	if _, err := worker.Retire(t.Context(), 32, DiagnosticMaterialReadBudget); err == nil {
		t.Fatal("failed receipt was acknowledged")
	}
	var header model.TrafficDiagnosticMaterial
	if err := db.First(&header, "traffic_record_id = ?", row.ID).Error; err != nil || header.RetiredAt != nil || !reflect.DeepEqual(loadDiagnosticSource(t, db, row.ID), before) {
		t.Fatal("retirement rollback lost material/facts", err)
	}
	if err := db.Exec("DROP TRIGGER reject_diagnostic_retirement").Error; err != nil {
		t.Fatal(err)
	}
	if out, err := worker.Retire(t.Context(), 32, DiagnosticMaterialReadBudget); err != nil || out.Retired != 1 {
		t.Fatal("explicit retry failed", out, err)
	}
}

type diagnosticBodyReads struct {
	logger.Interface
	bodies int
}

func (p *diagnosticBodyReads) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	query, _ := sql()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT SUBSTR(CAST(META AS ") {
		p.bodies++
	}
}

func TestSQLiteDiagnosticMaterialReadBudgetAndCorruptPrefixDoNotDiscardEvidence(t *testing.T) {
	t.Run("byte budget", func(t *testing.T) {
		db := diagnosticMaterialDB(t)
		for range 5 {
			createDiagnosticSource(t, db, strings.Repeat("m", nodeReportMetadataByteLimit), true)
		}
		probe := &diagnosticBodyReads{Interface: logger.Discard}
		worker := DiagnosticMaterialRetention{DB: db.Session(&gorm.Session{Logger: probe}), Now: func() time.Time { return time.Now().UTC().Add(8 * 24 * time.Hour) }}
		if out, err := worker.Retire(t.Context(), 32, DiagnosticMaterialReadBudget); err != nil || out.Retired != 4 || out.Bytes != 4*nodeReportMetadataByteLimit || probe.bodies != 4 {
			t.Fatal("unbounded body reads", out, probe.bodies, err)
		}
		if ready, err := worker.HasPending(t.Context()); err != nil || !ready {
			t.Fatal("remaining source disappeared", err)
		}
		if out, err := worker.Retire(t.Context(), 32, DiagnosticMaterialReadBudget); err != nil || out.Retired != 1 || probe.bodies != 5 {
			t.Fatal("budget continuation", out, probe.bodies, err)
		}
	})
	t.Run("corruption retains and does not starve", func(t *testing.T) {
		db := diagnosticMaterialDB(t)
		badHash := createDiagnosticSource(t, db, strings.Repeat("hash", 500), true)
		big := createDiagnosticSource(t, db, strings.Repeat("size", 500), true)
		native := createDiagnosticSource(t, db, strings.Repeat("native", 500), true)
		good := createDiagnosticSource(t, db, strings.Repeat("good", 500), true)
		if err := db.Model(&model.TrafficDiagnosticMaterial{}).Where("traffic_record_id = ?", badHash.ID).Update("source_sha256", strings.Repeat("0", 64)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.TrafficRecord{}).Where("id = ?", big.ID).UpdateColumn("meta", strings.Repeat("oversize", 128*1024)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.TrafficRecord{}).Where("id = ?", native.ID).UpdateColumn("event_type", "flow.updated").Error; err != nil {
			t.Fatal(err)
		}
		before := []model.TrafficRecord{loadDiagnosticSource(t, db, badHash.ID), loadDiagnosticSource(t, db, big.ID), loadDiagnosticSource(t, db, native.ID)}
		probe := &diagnosticBodyReads{Interface: logger.Discard}
		worker := DiagnosticMaterialRetention{DB: db.Session(&gorm.Session{Logger: probe}), Now: func() time.Time { return time.Now().UTC().Add(8 * 24 * time.Hour) }}
		out, err := worker.Retire(t.Context(), 32, DiagnosticMaterialReadBudget)
		if !errors.Is(err, ErrDiagnosticMaterial) || out.Retired != 1 || out.Inspected != 4 || probe.bodies != 2 {
			t.Fatal("corrupt prefix starved or allocated a large body", out, probe.bodies, err)
		}
		for _, row := range before {
			if !reflect.DeepEqual(loadDiagnosticSource(t, db, row.ID), row) {
				t.Fatal("corrupt/native evidence was discarded")
			}
		}
		if loadDiagnosticSource(t, db, good.ID).Meta == good.Meta {
			t.Fatal("valid source behind corrupt prefix was not retired")
		}
	})
}
