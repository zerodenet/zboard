package jobstore

import (
	"context"
	"strconv"

	"gorm.io/gorm"
)

// LocalWorkerConnectionCapacity is read-only. It reserves a main-process pool
// slot and, on MySQL, two worker connections plus the main pool's still-unused
// configured capacity. Missing status privileges stay unknown, not spare.
func LocalWorkerConnectionCapacity(ctx context.Context, db *gorm.DB) (known, available bool) {
	pool, err := db.DB()
	if err != nil {
		return false, false
	}
	stats := pool.Stats()
	if stats.MaxOpenConnections <= 0 {
		return false, false
	}
	spare := stats.MaxOpenConnections - stats.InUse
	if spare < 1 {
		return true, false
	}
	if db.Dialector.Name() == "sqlite" {
		return true, true
	}
	if db.Dialector.Name() != "mysql" {
		return false, false
	}
	var maximum uint64
	if err := db.WithContext(ctx).Raw("SELECT @@max_connections").Scan(&maximum).Error; err != nil || maximum == 0 {
		return false, false
	}
	var connected struct {
		VariableName string `gorm:"column:Variable_name"`
		Value        string `gorm:"column:Value"`
	}
	if err := db.WithContext(ctx).Raw("SHOW GLOBAL STATUS LIKE 'Threads_connected'").Scan(&connected).Error; err != nil || connected.VariableName != "Threads_connected" {
		return false, false
	}
	used, err := strconv.ParseUint(connected.Value, 10, 64)
	if err != nil || used > maximum {
		return false, false
	}
	// Reserve the main pool's full remaining budget and a two-connection
	// projection child. No server variable or configured pool is changed here.
	return true, maximum-used >= uint64(spare)+2
}
