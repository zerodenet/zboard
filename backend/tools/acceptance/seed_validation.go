package main

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func verifySeedHistory(db *gorm.DB, now time.Time, records int) error {
	if records == 0 {
		return nil
	}
	span := 6 * 24 * time.Hour
	var inWindow int64
	if err := db.Model(&model.TrafficRecord{}).Where("record_at >= ? AND record_at < ?", now.Add(-span), now).Count(&inWindow).Error; err != nil {
		return err
	}
	if inWindow != int64(records) {
		return fmt.Errorf("history fixture has %d/%d records in its six-day window", inWindow, records)
	}
	var last model.TrafficRecord
	if err := db.Select("record_at").Order("id desc").First(&last).Error; err != nil {
		return err
	}
	// Allow only the final interval plus integer-division rounding, not a
	// collapsed/overflowed distribution concentrated near the start date.
	if now.Sub(last.At) > span/time.Duration(records)+time.Duration(records)*time.Nanosecond {
		return fmt.Errorf("history fixture does not span the requested six days")
	}
	return nil
}
