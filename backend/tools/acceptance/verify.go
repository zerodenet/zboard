package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm/logger"
)

func verify(dir string) error {
	var f fixture
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	var expected struct {
		RunID string         `json:"run_id"`
		Bytes map[uint]int64 `json:"expected_bytes"`
	}
	data, err = os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &expected); err != nil {
		return err
	}
	if expected.RunID != f.RunID || f.RunID == "" {
		return fmt.Errorf("fixture run mismatch")
	}
	path := filepath.Join(dir, "zboard.db")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		return err
	}
	pool, _ := db.DB()
	defer pool.Close()
	db.Logger = logger.Default.LogMode(logger.Silent)
	var subscriptions []model.Subscription
	if err := db.Select("id", "flow_used").Find(&subscriptions).Error; err != nil {
		return err
	}
	actual := make(map[uint]int64, len(subscriptions))
	for _, sub := range subscriptions {
		actual[sub.ID] = sub.FlowUsed
	}
	var rows []struct {
		SubscriptionID uint
		Used           int64
	}
	if err := db.Model(&model.TrafficRecord{}).Select("subscription_id,SUM(used_bytes) AS used").Group("subscription_id").Scan(&rows).Error; err != nil {
		return err
	}
	ledger := make(map[uint]int64, len(rows))
	for _, row := range rows {
		ledger[row.SubscriptionID] = row.Used
	}
	var mismatches []string
	var expectedTotal, actualTotal, ledgerTotal int64
	for _, sub := range f.Subscriptions {
		want := sub.InitialBytes + expected.Bytes[sub.ID]
		expectedTotal += want
		actualTotal += actual[sub.ID]
		ledgerTotal += ledger[sub.ID]
		if actual[sub.ID] != want || ledger[sub.ID] != want {
			if len(mismatches) < 10 {
				mismatches = append(mismatches, fmt.Sprintf("subscription %d: expected=%d actual=%d ledger=%d", sub.ID, want, actual[sub.ID], ledger[sub.ID]))
			}
		}
	}
	if len(subscriptions) != len(f.Subscriptions) {
		mismatches = append(mismatches, "subscription count changed")
	}
	for id := range ledger {
		if _, exists := actual[id]; !exists {
			mismatches = append(mismatches, fmt.Sprintf("ledger references unexpected subscription %d", id))
		}
	}
	result := map[string]any{"run_id": f.RunID, "verified_at": time.Now().UTC(), "expected_total": expectedTotal, "subscription_total": actualTotal, "ledger_total": ledgerTotal, "mismatches": mismatches, "passed": len(mismatches) == 0}
	if err := writeJSON(filepath.Join(dir, "verification.json"), result); err != nil {
		return err
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("accounting has not reconciled: %v", mismatches)
	}
	return nil
}
