package meteringstore

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type hourlyUsageKey struct {
	RecordAt                                           string
	UserID, SubscriptionID, NodeID, ProtocolEndpointID uint
	ProtocolMultiplierMilli                            int64
}

// RecordUsageProjections runs only after successful ledger insertion, in the
// same accounting transaction. Duplicate reports never reach this boundary.
func RecordUsageProjections(tx *gorm.DB, records []model.TrafficRecord) error {
	if err := addMySQLTrafficHourly(tx, records); err != nil {
		return err
	}
	return AddProtocolEndpointUsage(tx, records)
}

func addMySQLTrafficHourly(tx *gorm.DB, records []model.TrafficRecord) error {
	// SQLite triggers also cover SQL replacements/updates; don't count twice.
	if datastore.IsSQLite(tx) || len(records) == 0 {
		return nil
	}
	rows := make(map[hourlyUsageKey]map[string]any)
	keys := make([]hourlyUsageKey, 0)
	for _, record := range records {
		key := hourlyUsageKey{record.At.UTC().Truncate(time.Hour).Format("2006-01-02 15:04:05"), record.UserID, record.SubscriptionID, record.NodeID, record.ProtocolEndpointID, record.ProtocolMultiplierMilli}
		row, ok := rows[key]
		if !ok {
			row = map[string]any{"record_at": key.RecordAt, "user_id": key.UserID, "subscription_id": key.SubscriptionID, "node_id": key.NodeID, "protocol_endpoint_id": key.ProtocolEndpointID, "protocol_multiplier_milli": key.ProtocolMultiplierMilli,
				"raw_bytes": int64(0), "upload_bytes": int64(0), "download_bytes": int64(0), "used_bytes": int64(0), "record_count": int64(0)}
			rows[key] = row
			keys = append(keys, key)
		}
		for column, value := range map[string]int64{"raw_bytes": record.RawBytes, "upload_bytes": record.UploadBytes, "download_bytes": record.DownloadBytes, "used_bytes": record.UsedBytes, "record_count": 1} {
			previous := row[column].(int64)
			if (value > 0 && previous > math.MaxInt64-value) || (value < 0 && previous < math.MinInt64-value) {
				return fmt.Errorf("traffic hourly %s overflow", column)
			}
			row[column] = previous + value
		}
	}
	// All writers acquire bucket locks in the same order.
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.RecordAt != b.RecordAt {
			return a.RecordAt < b.RecordAt
		}
		if a.UserID != b.UserID {
			return a.UserID < b.UserID
		}
		if a.SubscriptionID != b.SubscriptionID {
			return a.SubscriptionID < b.SubscriptionID
		}
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		if a.ProtocolEndpointID != b.ProtocolEndpointID {
			return a.ProtocolEndpointID < b.ProtocolEndpointID
		}
		return a.ProtocolMultiplierMilli < b.ProtocolMultiplierMilli
	})
	batch := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		batch = append(batch, rows[key])
	}
	// MySQL VALUES() binds each inserted delta, including batched buckets.
	assignments := make(map[string]any)
	for _, column := range []string{"raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count"} {
		assignments[column] = gorm.Expr(column + " + VALUES(" + column + ")")
	}
	return tx.Table("traffic_usage_hourly").Clauses(clause.OnConflict{DoUpdates: clause.Assignments(assignments)}).CreateInBatches(&batch, 100).Error
}
