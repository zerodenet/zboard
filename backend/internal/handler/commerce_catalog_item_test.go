package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNewPlanCatalogItemUsesPlanEntitlements(t *testing.T) {
	plan := model.Plan{
		TrafficBytes:   1024,
		DeviceLimit:    3,
		SpeedLimitMbps: 50,
	}
	primarySKU := &model.PlanSKU{ID: 7, PriceCents: 1000}

	item := newPlanCatalogItem(plan, planSKUCountRow{}, primarySKU)
	if item.TrafficBytes != plan.TrafficBytes {
		t.Fatalf("traffic bytes = %d, want %d", item.TrafficBytes, plan.TrafficBytes)
	}
	if item.DeviceLimit != plan.DeviceLimit {
		t.Fatalf("device limit = %d, want %d", item.DeviceLimit, plan.DeviceLimit)
	}
	if item.SpeedLimitMbps != plan.SpeedLimitMbps {
		t.Fatalf("speed limit = %d, want %d", item.SpeedLimitMbps, plan.SpeedLimitMbps)
	}
	if item.PrimarySKU != primarySKU {
		t.Fatal("primary SKU was not preserved")
	}
}
