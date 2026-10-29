package commercestore

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Current valuation includes paid sources ending after the current term began.
// Pending attempts and surviving holds must also close atomically. Earlier
// completed history remains immutable and cannot consume this authority budget.
// LIMIT bounds returned candidates, not the database's total scanned history.
func terminationOrders(db *gorm.DB, owner, subscription uint, start time.Time) *gorm.DB {
	held := db.Model(&model.OrderFunding{}).Select("1").Where("order_fundings.order_id = orders.id AND order_fundings.state = ?", "reserved")
	return db.Model(&model.Order{}).
		Where("user_id = ? AND (subscription_id = ? OR target_subscription_id = ?)", owner, subscription, subscription).
		Where("subscription_ended_at IS NULL OR subscription_ended_at > ? OR status = ? OR (status IN ? AND EXISTS (?))", start, "pending", []string{"canceled", "failed"}, held).
		Order("id ASC")
}
