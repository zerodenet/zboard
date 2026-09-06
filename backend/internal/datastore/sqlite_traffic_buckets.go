package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

// SQLiteTrafficBucketExpression is shared by the schema and read queries:
// SQLite only uses an expression index when the expressions match.
func SQLiteTrafficBucketExpression(bucket string) string {
	format := "%Y-%m-%d %H:%M:00"
	switch bucket {
	case "minute":
	case "hour":
		format = "%Y-%m-%d %H:00:00"
	case "day":
		format = "%Y-%m-%d 00:00:00"
	default:
		return ""
	}
	return "strftime('" + format + "', record_at)"
}

func reconcileSQLiteTrafficBuckets(db *gorm.DB) error {
	if !db.Migrator().HasTable("traffic_records") {
		return nil
	}
	for _, bucket := range []string{"minute", "hour", "day"} {
		statement := "CREATE INDEX IF NOT EXISTS idx_traffic_records_" + bucket + "_groups ON traffic_records (" +
			SQLiteTrafficBucketExpression(bucket) + ", user_id, COALESCE(subscription_id, 0), node_id, protocol_multiplier_milli, record_at)"
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("add SQLite %s bucket index: %w", bucket, err)
		}
	}
	return nil
}
