package meteringstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Activity is a replaceable UI hint, never financial authority. Coalesce it to
// a minute and skip an administratively busy credential. A later report repairs
// the hint; failure must never make a committed charge appear uncommitted.
func TouchCredentialActivity(ctx context.Context, db *gorm.DB, ids []uint) {
	if len(ids) == 0 {
		return
	}
	now := time.Now().UTC().Truncate(time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	seen := map[uint]bool{}
	for _, id := range ids {
		if seen[id] || ctx.Err() != nil {
			continue
		}
		seen[id] = true
		_ = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var c model.ProtocolCredential
			q := tx.Select("id").Where("id=? AND (last_used_at IS NULL OR last_used_at<?)", id, now)
			if tx.Dialector.Name() == "mysql" {
				q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
			}
			r := q.Find(&c)
			if r.Error != nil || r.RowsAffected == 0 {
				return r.Error
			}
			return tx.Model(&model.ProtocolCredential{}).Where("id=? AND (last_used_at IS NULL OR last_used_at<?)", id, now).UpdateColumn("last_used_at", now).Error
		})
	}
}
