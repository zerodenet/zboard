package handler

import (
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestTrafficFirstPagePreservesPartialBucketsAndFallsBack(t *testing.T) {
	f := newTrafficReadFixture(t)
	checkTrafficFirstPage(t, f.h.db)
}

func TestMySQLTrafficFirstPagePreservesPartialBucketsAndFallsBack(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	checkTrafficFirstPage(t, h.db)
}

func checkTrafficFirstPage(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE first_page_fixture (
id INTEGER PRIMARY KEY, record_at DATETIME NOT NULL, user_id INTEGER,
subscription_id INTEGER, node_id INTEGER, protocol_multiplier_milli INTEGER,
used_bytes BIGINT NOT NULL, raw_bytes BIGINT NOT NULL DEFAULT 0,
upload_bytes BIGINT NOT NULL DEFAULT 0, download_bytes BIGINT NOT NULL DEFAULT 0,
protocol_endpoint_id INTEGER NOT NULL DEFAULT 1)`).Error; err != nil {
		t.Fatal(err)
	}
	type row struct {
		ID                      int
		At                      time.Time `gorm:"column:record_at"`
		UserID                  uint
		SubscriptionID          *uint
		NodeID                  uint
		ProtocolMultiplierMilli int64
		UsedBytes               int64
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, manyGroups := range []bool{true, false} {
		if err := db.Exec("DELETE FROM first_page_fixture").Error; err != nil {
			t.Fatal(err)
		}
		var rows []row
		for i := 0; i < trafficFirstPageProbeRows+32; i++ {
			subID := uint(i % 67)
			at := start.Add(12*time.Hour + 55*time.Minute + 30*time.Second)
			if i < 32 {
				at = at.Add(-20 * time.Second)
			}
			if !manyGroups {
				if i < 32 {
					at = start.Add(time.Duration(i) * time.Minute)
				} else {
					subID = 1 // Probe has one group; earlier groups must survive.
				}
			}
			sub := &subID
			if subID == 0 && i%2 == 0 {
				sub = nil // NULL and zero share a logical bucket.
			}
			rows = append(rows, row{i + 1, at, 1, sub, 1, 1000, int64(i + 1)})
		}
		// Same-account rows outside the requested window must not influence
		// either the probe or the final page after its lower bound is replaced.
		for _, at := range []time.Time{start.Add(-time.Second), start.Add(24 * time.Hour), start.Add(25 * time.Hour)} {
			sub := uint(99999)
			rows = append(rows, row{len(rows) + 1, at, 1, &sub, 1, 1000, 99999})
		}
		// A different account dominates the most recent records. Applying scope
		// after the probe instead of before it would produce an incorrect bound.
		for i := 0; i < trafficFirstPageProbeRows+1; i++ {
			sub := uint(i)
			rows = append(rows, row{len(rows) + 1, start.Add(20 * time.Hour), 2, &sub, 1, 1000, 99999})
		}
		if err := db.Table("first_page_fixture").CreateInBatches(&rows, 100).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE first_page_fixture SET raw_bytes = used_bytes * 2, upload_bytes = used_bytes, download_bytes = used_bytes, protocol_endpoint_id = id % 2 + 1").Error; err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			"UPDATE first_page_fixture SET node_id = NULL WHERE id = 80",
			"UPDATE first_page_fixture SET protocol_multiplier_milli = NULL WHERE id = 81",
			"UPDATE first_page_fixture SET user_id = NULL WHERE id = 82",
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"minute", "hour", "day"} {
			bucket, _ := parseTrafficUsageBucket(name)
			bucket = bucket.forDB(db)
			for _, from := range []time.Time{start, start.Add(12*time.Hour + 55*time.Minute + 15*time.Second)} {
				for _, to := range []time.Time{start.Add(24 * time.Hour), start.Add(12*time.Hour + 55*time.Minute + 25*time.Second)} {
					if !from.Before(to) {
						continue
					}
					window := historyWindow{From: from, To: to}
					base := db.Table("first_page_fixture").Where("user_id = ?", 1)
					type result = trafficUsageBucket
					read := func(source *gorm.DB) []result {
						t.Helper()
						var values []result
						if err := source.Select("MIN(id) AS id, user_id, COALESCE(subscription_id, 0) AS subscription_id, node_id, protocol_multiplier_milli, SUM(raw_bytes) AS raw_bytes, SUM(upload_bytes) AS upload_bytes, SUM(download_bytes) AS download_bytes, SUM(used_bytes) AS used_bytes, COUNT(*) AS record_count, " + bucket.Expression + " AS record_at").
							Group(bucket.group()).Order("record_at desc, id desc").Limit(4).Scan(&values).Error; err != nil {
							t.Fatal(err)
						}
						return values
					}
					want := read(applyHistoryWindow(base.Session(&gorm.Session{}), "record_at", window))
					got := read(bucket.firstPageSource(base, window, 3))
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("page changed: many_groups=%v bucket=%s from=%s got=%+v want=%+v", manyGroups, name, from, got, want)
					}
					if db.Dialector.Name() == "sqlite" {
						for _, filter := range []string{"user_id = 1", "user_id = 1 AND protocol_endpoint_id = 1", "user_id = 1 AND subscription_id = 1", "node_id = 2", "node_id IS NULL", "protocol_multiplier_milli IS NULL", "user_id IS NULL"} {
							scope := db.Table("first_page_fixture").Where(filter)
							windowed := applyHistoryWindow(scope.Session(&gorm.Session{}), "record_at", window)
							want := read(windowed.Session(&gorm.Session{}))
							var selected []result
							if err := bucket.selectedFirstPageQuery(windowed, bucket.firstPageSource(scope, window, 3), 3).
								Order("record_at desc, id desc").Scan(&selected).Error; err != nil {
								t.Fatal(err)
							}
							if !reflect.DeepEqual(selected, want) {
								t.Fatalf("selected page changed: filter=%s bucket=%s from=%s to=%s got=%+v want=%+v", filter, name, from, to, selected, want)
							}
						}
					}
				}
			}
		}
	}
}
