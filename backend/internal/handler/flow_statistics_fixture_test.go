package handler

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"gorm.io/gorm"
	"testing"
)

func drainFlowStatisticsFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	worker := meteringstore.FlowStatistics{DB: db}
	for i := 0; i < 100; i++ {
		ready, err := worker.HasPending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !ready {
			return
		}
		if _, err := worker.Drain(t.Context(), 200); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("bounded flow statistics fixture drain incomplete")
}
