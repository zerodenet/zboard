package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestTrafficBucketCountsUseIndexAndKeepExactTimeWindow(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	window := historyWindow{From: start.Add(time.Hour + 5*time.Minute), To: start.Add(time.Hour + 55*time.Minute)}
	for _, name := range []string{"minute", "hour", "day"} {
		bucket, _ := parseTrafficUsageBucket(name)
		bucket = bucket.forDB(f.h.db)
		f.capture()
		base := applyHistoryWindow(f.h.db.Model(&model.TrafficRecord{}), "record_at", window)
		stats, err := loadTrafficUsageStatistics(base, bucket, window)
		if err != nil || stats.Total != 2 || stats.Aggregates.UsedBytes != 40 {
			t.Fatalf("%s included rows outside the exact window: %+v %v", name, stats, err)
		}
		if len(f.log.queries) != 2 {
			t.Fatalf("unexpected statistics queries: %v", f.log.queries)
		}
		var steps []struct{ Detail string }
		if err := f.h.db.Raw("EXPLAIN QUERY PLAN " + f.log.queries[1]).Scan(&steps).Error; err != nil {
			t.Fatal(err)
		}
		indexed := false
		for _, step := range steps {
			indexed = indexed || strings.Contains(step.Detail, "idx_traffic_records_"+name+"_groups")
			if strings.Contains(step.Detail, "TEMP B-TREE FOR GROUP BY") {
				t.Fatalf("%s still sorts the complete history: %v", name, steps)
			}
		}
		if !indexed {
			t.Fatalf("%s did not use bucket index: %v", name, steps)
		}
	}
}

func TestTrafficFirstPageStreamsGroupsFromProbeBoundary(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	window := historyWindow{From: start.Add(time.Hour + 5*time.Minute), To: start.Add(time.Hour + 55*time.Minute)}
	for _, name := range []string{"minute", "hour", "day"} {
		bucket, _ := parseTrafficUsageBucket(name)
		bucket = bucket.forDB(f.h.db)
		source := bucket.firstPageSource(f.h.db.Model(&model.TrafficRecord{}), window, 1)
		query := source.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Select("MIN(id) AS id, SUM(used_bytes) AS used_bytes, " + bucket.Expression + " AS record_at").
				Group(bucket.group()).Order("record_at desc, id desc").Limit(2).Find(&[]trafficUsageBucket{})
		})
		var steps []struct {
			Parent int
			Detail string
		}
		if err := f.h.db.Raw("EXPLAIN QUERY PLAN " + query).Scan(&steps).Error; err != nil {
			t.Fatal(err)
		}
		indexed := false
		for _, step := range steps {
			if step.Parent != 0 {
				continue
			} // The bounded probe may sort its 1,024 rows.
			indexed = indexed || strings.Contains(step.Detail, "idx_traffic_records_"+name+"_groups")
			if strings.Contains(step.Detail, "TEMP B-TREE FOR GROUP BY") {
				t.Fatalf("%s sorts the complete page source: %v", name, steps)
			}
		}
		if !indexed {
			t.Fatalf("%s does not seek the grouping index: %v", name, steps)
		}
	}
}

func TestTrafficSelectedPageSeeksEachSelectedGroup(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	window := historyWindow{From: start, To: start.Add(24 * time.Hour)}
	for _, name := range []string{"minute", "hour", "day"} {
		bucket, _ := parseTrafficUsageBucket(name)
		bucket = bucket.forDB(f.h.db)
		scope := f.h.db.Model(&model.TrafficRecord{})
		query := bucket.selectedFirstPageQuery(applyHistoryWindow(scope.Session(&gorm.Session{}), "record_at", window), bucket.firstPageSource(scope, window, 1), 1).
			ToSQL(func(tx *gorm.DB) *gorm.DB {
				return tx.Order("record_at desc, id desc").Limit(2).Find(&[]trafficUsageBucket{})
			})
		var steps []struct{ Detail string }
		if err := f.h.db.Raw("EXPLAIN QUERY PLAN " + query).Scan(&steps).Error; err != nil {
			t.Fatal(err)
		}
		seek := false
		for _, step := range steps {
			seek = seek || (strings.Contains(step.Detail, "idx_traffic_records_"+name+"_groups") && strings.Contains(step.Detail, "<expr>=? AND user_id=?"))
		}
		if !seek {
			t.Fatalf("%s did not seek selected group dimensions: %v", name, steps)
		}
	}
}
