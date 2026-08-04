package model

import (
	"errors"

	"gorm.io/gorm"
)

// AfterFind hydrates the legacy, JSON-hidden entitlement fields for periodic
// SKUs from their owning Plan. Plan remains the authoritative source; the
// values are populated only so catalog serializers can expose product-level
// entitlements without restoring editable entitlement fields on PlanSKU.
func (sku *PlanSKU) AfterFind(tx *gorm.DB) error {
	if sku == nil || sku.PlanID == 0 || sku.BillingUnit == "once" || sku.SKUType == "traffic_pack" {
		return nil
	}

	var entitlement struct {
		TrafficBytes   int64
		DeviceLimit    int
		SpeedLimitMbps int
	}
	if err := tx.Model(&Plan{}).
		Select("traffic_bytes", "device_limit", "speed_limit_mbps").
		Where("id = ?", sku.PlanID).
		Take(&entitlement).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	sku.TrafficBytes = entitlement.TrafficBytes
	sku.DeviceLimit = entitlement.DeviceLimit
	sku.SpeedLimitMbps = entitlement.SpeedLimitMbps
	return nil
}
