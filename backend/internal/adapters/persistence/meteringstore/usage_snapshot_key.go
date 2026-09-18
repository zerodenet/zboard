package meteringstore

import (
	"crypto/sha256"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Build the key from the validated, fully scoped query without executing SQL.
// This includes the account, all filters and exact time boundaries, and omits
// pagination. Hashing keeps cache keys bounded even if query construction grows.
func UsageSnapshotQueryKey(query *gorm.DB, kind string) [32]byte {
	sql := query.Session(&gorm.Session{Logger: logger.Discard}).ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Select("1").Find(&[]model.TrafficRecord{})
	})
	return sha256.Sum256([]byte(kind + "\x00" + sql))
}
