package handler

import "encoding/json"

// MarshalJSON keeps pricing on the primary SKU while exposing subscription
// entitlements at the product level. The PlanSKU fields used here are hydrated
// from Plan by the model hook and remain hidden from the SKU JSON itself.
func (item planCatalogItem) MarshalJSON() ([]byte, error) {
	type catalogAlias planCatalogItem
	payload := struct {
		catalogAlias
		TrafficBytes   int64 `json:"traffic_bytes"`
		SpeedLimitMbps int   `json:"speed_limit_mbps"`
		DeviceLimit    int   `json:"device_limit"`
	}{
		catalogAlias: catalogAlias(item),
	}
	if item.PrimarySKU != nil {
		payload.TrafficBytes = item.PrimarySKU.TrafficBytes
		payload.SpeedLimitMbps = item.PrimarySKU.SpeedLimitMbps
		payload.DeviceLimit = item.PrimarySKU.DeviceLimit
	}
	return json.Marshal(payload)
}
