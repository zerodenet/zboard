package datastore_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func TestMySQLPrincipalRuntimeBackfillBatchesPreserveOriginalFacts(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	at := time.Now().UTC().Truncate(time.Millisecond)
	want := make([]meteringstore.PrincipalFlowObservation, 1537)
	for i := range want {
		raw := fmt.Sprintf("运行代次-\x00-%d-é", i)
		want[i] = meteringstore.PrincipalFlowObservation{NodeID: 73, CoreInstanceID: raw, SessionRegistryRevision: uint64(i + 1), EventID: fmt.Sprintf("event-%d", i), Sequence: uint64(i + 1), PrincipalKey: "principal", ActiveFlows: uint64(i % 17), ObservedAt: at, CreatedAt: at}
		if i%7 == 0 {
			want[i].CoreInstanceSHA256 = metering.RuntimeIdentityDigest(raw)
		}
	}
	if err := db.CreateInBatches(&want, 128).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", principalRuntimeVersion).Error; err != nil {
		t.Fatal(err)
	}
	updates := principalRuntimeBackfillUpdateCounter(t, db)
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if *updates != 4 {
		t.Fatalf("digest UPDATE statements = %d, want four bounded batches for 1537 rows", *updates)
	}
	var got []meteringstore.PrincipalFlowObservation
	if err := db.Order("id").Find(&got).Error; err != nil || len(got) != len(want) {
		t.Fatal("backfill changed observation inventory", len(got), err)
	}
	for i := range want {
		want[i].CoreInstanceSHA256 = metering.RuntimeIdentityDigest(want[i].CoreInstanceID)
		assertPrincipalObservationSnapshot(t, want[i], got[i])
	}
	if err := datastore.RunMigrations(db); err != nil || *updates != 4 {
		t.Fatal("completed backfill repeated writes", *updates, err)
	}
}

func TestMySQLPrincipalRuntimeBackfillBoundsSourceBytes(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	at := time.Now().UTC().Truncate(time.Millisecond)
	rows := make([]meteringstore.PrincipalFlowObservation, 3)
	for i := range rows {
		rows[i] = meteringstore.PrincipalFlowObservation{NodeID: 73, CoreInstanceID: strings.Repeat("x", metering.MaxRuntimeIdentityBytes-1) + fmt.Sprint(i), SessionRegistryRevision: uint64(i + 1), EventID: fmt.Sprint(i), PrincipalKey: "principal", ObservedAt: at, CreatedAt: at}
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", principalRuntimeVersion).Error; err != nil {
		t.Fatal(err)
	}
	updates := principalRuntimeBackfillUpdateCounter(t, db)
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if *updates != 2 {
		t.Fatalf("digest UPDATE statements = %d, want two batches under the 4MiB source budget", *updates)
	}
}

func TestMySQLPrincipalRuntimeBackfillRejectsMismatchBeforeBatchWrites(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	at := time.Now().UTC().Truncate(time.Millisecond)
	rows := []meteringstore.PrincipalFlowObservation{
		{NodeID: 73, CoreInstanceID: "original-empty", SessionRegistryRevision: 1, EventID: "good", PrincipalKey: "principal", ObservedAt: at, CreatedAt: at},
		{NodeID: 73, CoreInstanceID: "original-conflicting", CoreInstanceSHA256: strings.ToUpper(metering.RuntimeIdentityDigest("original-conflicting")), SessionRegistryRevision: 2, EventID: "bad", PrincipalKey: "principal", ObservedAt: at, CreatedAt: at},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", principalRuntimeVersion).Error; err != nil {
		t.Fatal(err)
	}
	updates := principalRuntimeBackfillUpdateCounter(t, db)
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "raw/digest mismatch") {
		t.Fatal("conflicting original digest accepted", err)
	}
	if *updates != 0 {
		t.Fatal("batch wrote digests before checking every original", *updates)
	}
	var got meteringstore.PrincipalFlowObservation
	if err := db.First(&got, rows[0].ID).Error; err != nil || got.CoreInstanceSHA256 != "" {
		t.Fatal("failed batch changed an original empty digest", got.CoreInstanceSHA256, err)
	}
}

func principalRuntimeBackfillUpdateCounter(t *testing.T, db *gorm.DB) *int {
	t.Helper()
	count := new(int)
	const name = "test:principal-runtime-backfill-updates"
	if err := db.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_flow_observations" {
			*count++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(name) })
	return count
}
