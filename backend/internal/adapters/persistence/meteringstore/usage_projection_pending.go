package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const UsageProjectionBatchLimit = 200

// EnqueueUsageProjections shares the ledger/quota transaction. Failure is a
// settlement failure, never a successful in-memory acknowledgement. Call only
// for newly inserted ledger records, after the database assigns their IDs.
func EnqueueUsageProjections(tx *gorm.DB, records []model.TrafficRecord) error {
	rows := make([]model.MeteringUsageProjectionPending, 0, len(records))
	now := time.Now().UTC()
	for _, record := range records {
		if record.ID == 0 {
			return errors.New("usage projection requires a persisted traffic record ID")
		}
		recordAt := record.At.UTC().Truncate(time.Hour)
		rows = append(rows, model.MeteringUsageProjectionPending{TrafficRecordID: record.ID, ProtocolEndpointID: record.ProtocolEndpointID, CreatedAt: now,
			HourlyPending: true, UserID: record.UserID, SubscriptionID: record.SubscriptionID, NodeID: record.NodeID, RecordAt: &recordAt})
	}
	if len(rows) == 0 {
		return nil
	}
	// No conflict-ignore: enqueueing an already projected fact would apply its
	// delta twice. Idempotency belongs to the authoritative report insertion.
	return tx.CreateInBatches(&rows, 100).Error
}

type UsageProjectionWorker struct{ DB *gorm.DB }

func (s UsageProjectionWorker) HasPending(ctx context.Context) (bool, error) {
	var row model.MeteringUsageProjectionPending
	result := s.DB.WithContext(ctx).Order("traffic_record_id").Limit(1).Find(&row)
	return result.RowsAffected > 0, result.Error
}

// Drain applies at most one bounded batch. The source facts and quota are
// already committed. Projection writes and checkpoint removal share a separate
// transaction, so failed writes, failed deletion, crashes and concurrent workers
// leave either the whole batch pending or its required hourly/daily deltas
// committed. Legacy intents require only their still-outstanding daily delta.
func (s UsageProjectionWorker) Drain(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > UsageProjectionBatchLimit {
		return 0, fmt.Errorf("usage projection batch limit must be between 1 and %d", UsageProjectionBatchLimit)
	}
	processed := 0
	// On MySQL, READ COMMITTED avoids locking the queue's supremum gap when
	// fewer than limit rows exist. A slow auxiliary upsert must not block the
	// core writer from inserting new durable intents. Pending row locks still
	// protect exact-once projection. SQLite retains its single-writer boundary.
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var pending []model.MeteringUsageProjectionPending
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("traffic_record_id").Limit(limit).Find(&pending).Error; err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(pending))
		for _, row := range pending {
			ids = append(ids, row.TrafficRecordID)
		}
		var records []model.TrafficRecord
		if err := tx.Where("id IN ?", ids).Order("id").Find(&records).Error; err != nil {
			return err
		}
		if len(records) != len(pending) {
			return errors.New("pending usage projection source traffic record is missing")
		}
		hourly := make([]model.TrafficRecord, 0, len(records))
		for i, record := range records {
			if record.ID != pending[i].TrafficRecordID || record.ProtocolEndpointID != pending[i].ProtocolEndpointID {
				return errors.New("pending usage projection source identity changed")
			}
			if pending[i].HourlyPending {
				p := pending[i]
				if p.UserID != record.UserID || p.SubscriptionID != record.SubscriptionID || p.NodeID != record.NodeID || p.RecordAt == nil || !p.RecordAt.Equal(record.At.UTC().Truncate(time.Hour)) {
					return errors.New("pending hourly projection source scope or time changed")
				}
				hourly = append(hourly, record)
			}
		}
		if err := addTrafficHourly(tx, hourly); err != nil {
			return err
		}
		if err := AddProtocolEndpointUsage(tx, records); err != nil {
			return err
		}
		deleted := tx.Where("traffic_record_id IN ?", ids).Delete(&model.MeteringUsageProjectionPending{})
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected != int64(len(ids)) {
			return errors.New("usage projection checkpoint changed during processing")
		}
		processed = len(ids)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	return processed, nil
}
