package meteringstore

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestLegacyPrincipalRetentionIsBoundedAndPreservesCurrentAuthority(t *testing.T) {
	db := trafficAggregateDB(t)
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	for i := 1; i <= 5; i++ {
		r := PrincipalFlowObservation{NodeID: 1, EventID: fmt.Sprint(i), CoreInstanceID: "old", CoreInstanceSHA256: fmt.Sprintf("%064d", i), PrincipalKey: fmt.Sprint(i), SessionRegistryRevision: uint64(i), ObservedAt: old, CreatedAt: old}
		if i == 5 {
			r.CreatedAt = now
		}
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	current := PrincipalFlowCurrent{NodeID: 1, PrincipalKey: "current", CoreInstanceID: "live", SessionRegistryRevision: 9, ActiveFlows: 4, ObservedAt: now, UpdatedAt: now}
	if err := db.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	gen := PrincipalFlowNodeGeneration{NodeID: 1, CoreInstanceID: "live", StartedAt: old, UpdatedAt: now}
	if err := db.Create(&gen).Error; err != nil {
		t.Fatal(err)
	}
	s := LegacyPrincipalRetention{DB: db, Now: func() time.Time { return now }}
	fault := errors.New("delete failed")
	db.Callback().Delete().Before("gorm:delete").Register("test:legacy_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_flow_observations" {
			tx.AddError(fault)
		}
	})
	if _, err := s.Drain(t.Context(), 2); !errors.Is(err, fault) {
		t.Fatal(err)
	}
	db.Callback().Delete().Remove("test:legacy_fail")
	if statisticsCount(t, db, "principal_flow_observations") != 5 {
		t.Fatal("failed deletion committed")
	}
	for i := 0; i < 2; i++ {
		if n, err := s.Drain(t.Context(), 2); err != nil || n != 2 {
			t.Fatal(n, err)
		}
	}
	if ready, err := s.HasPending(t.Context()); err != nil || ready {
		t.Fatal("recent diagnostic was eligible", ready, err)
	}
	var got PrincipalFlowCurrent
	if err := db.First(&got).Error; err != nil || got.SessionRegistryRevision != current.SessionRegistryRevision || got.ActiveFlows != 4 {
		t.Fatal("current authority changed", got, err)
	}
	if statisticsCount(t, db, "principal_flow_node_generations") != 1 {
		t.Fatal("generation floor removed")
	}
}
