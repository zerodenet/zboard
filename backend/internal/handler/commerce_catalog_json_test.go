package handler

import (
	"encoding/json"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPlanCatalogItemMarshalJSONExposesPlanEntitlements(t *testing.T) {
	item := planCatalogItem{
		PrimarySKU: &model.PlanSKU{
			TrafficBytes:   1024,
			DeviceLimit:    3,
			SpeedLimitMbps: 50,
		},
	}

	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal catalog item: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode catalog item: %v", err)
	}
	if decoded["traffic_bytes"] != float64(1024) {
		t.Fatalf("traffic_bytes = %#v", decoded["traffic_bytes"])
	}
	if decoded["device_limit"] != float64(3) {
		t.Fatalf("device_limit = %#v", decoded["device_limit"])
	}
	if decoded["speed_limit_mbps"] != float64(50) {
		t.Fatalf("speed_limit_mbps = %#v", decoded["speed_limit_mbps"])
	}
}
