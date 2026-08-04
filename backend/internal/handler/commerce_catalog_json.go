package handler

import "encoding/json"

// MarshalJSON keeps price and billing information on the primary SKU while
// exposing the owning Plan's subscription entitlements at the product level.
// The PlanSKU fields used as serialization context are hydrated from Plan and
// remain hidden from the nested SKU JSON, so they cannot be mistaken for SKU
// overrides by API consumers.
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
