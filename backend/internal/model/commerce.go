package model

import "time"

// PlanSKUOperation separates the commercial situations in which a price can
// be used from the price itself. A single SKU can therefore be available for
// purchase, renewal and plan changes without duplicating its price and
// entitlement definition.
type PlanSKUOperation struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	PlanSKUID uint      `json:"plan_sku_id" gorm:"not null;index;uniqueIndex:uk_plan_sku_operations_sku_operation"`
	Operation string    `json:"operation" gorm:"size:20;not null;uniqueIndex:uk_plan_sku_operations_sku_operation"`
	CreatedAt time.Time `json:"created_at"`
}

func (PlanSKUOperation) TableName() string {
	return "plan_sku_operations"
}
