package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestProjectionFlowStatisticsBacklogUsesBoundedRegistrationAndPending(t *testing.T) {
	db := schemaFixture(t, datastore.DriverSQLite)
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Minute)
	old := now.Add(-time.Hour).Truncate(time.Millisecond)
	raw := meteringstore.FlowStartRecord{NodeID: 2, SubscriptionID: 4, EventID: "pending-candidate", OccurredAt: old, ReceivedAt: old, CreatedAt: old}
	if err := db.Create(&raw).Error; err != nil {
		t.Fatal(err)
	}
	var statements []string
	const callback = "test:thin_flow_statistics_capacity"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) { statements = append(statements, tx.Statement.SQL.String()) }); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	found, at, err := projectionFlowStartStatisticsBacklog(t.Context(), db, now)
	if err != nil || !found || !at.Equal(old) {
		t.Fatal(found, at, err)
	}
	if len(statements) != 3 {
		t.Fatal("candidate reads not fixed", statements)
	}
	for _, sql := range statements {
		upper := strings.ToUpper(sql)
		if strings.Contains(upper, "COUNT(") || strings.Contains(upper, "SELECT *") || !strings.Contains(upper, "LIMIT 1") {
			t.Fatal("unbounded statistics backlog", sql)
		}
	}
	if err := db.Table("subscription_flow_start_events").Where("id=?", raw.ID).Update("statistics_registered", true).Error; err != nil {
		t.Fatal(err)
	}
	if found, _, err := projectionFlowStartStatisticsBacklog(t.Context(), db, now); err != nil || found {
		t.Fatal("registered source created fake backlog", found, err)
	}
	pending := model.FlowStartStatisticsPending{SourceID: uint64(raw.ID), SubscriptionID: 4, NodeID: 2, OccurredAt: old, ReceivedAt: old, CreatedAt: old}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if found, at, err := projectionFlowStartStatisticsBacklog(t.Context(), db, now); err != nil || !found || !at.Equal(old) {
		t.Fatal("durable pending omitted", found, at, err)
	}
	if err := db.Model(&pending).Update("created_at", now.Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if found, _, err := projectionFlowStartStatisticsBacklog(t.Context(), db, now); err == nil || found {
		t.Fatal("future time admitted as known idle", found, err)
	}
	if err := db.Delete(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id=?", 1).Delete(&model.FlowStartStatisticsState{}).Error; err != nil {
		t.Fatal(err)
	}
	if found, _, err := projectionFlowStartStatisticsBacklog(t.Context(), db, now); err == nil || found {
		t.Fatal("missing checkpoint interpreted as zero", found, err)
	}
}
