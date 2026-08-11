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

func TestPlanCatalogItemPreservesAdministrativeSummaryFields(t *testing.T) {
	plan := model.Plan{
		ID:             11,
		Name:           "Starter",
		Slug:           "starter",
		Revision:       7,
		TrafficBytes:   2048,
		DeviceLimit:    5,
		SpeedLimitMbps: 100,
	}
	counts := planSKUCountRow{PlanID: 11, SKUCount: 4, ActiveSKUCount: 2}

	item := newPlanCatalogItem(plan, counts, nil)
	if item.ID != plan.ID || item.Revision != plan.Revision {
		t.Fatalf("catalog summary identity = id %d revision %d, want id %d revision %d", item.ID, item.Revision, plan.ID, plan.Revision)
	}
	if item.SKUCount != counts.SKUCount || item.ActiveSKUCount != counts.ActiveSKUCount {
		t.Fatalf("catalog SKU counts = %d/%d, want %d/%d", item.SKUCount, item.ActiveSKUCount, counts.SKUCount, counts.ActiveSKUCount)
	}
	if item.TrafficBytes != plan.TrafficBytes || item.DeviceLimit != plan.DeviceLimit || item.SpeedLimitMbps != plan.SpeedLimitMbps {
		t.Fatalf("catalog entitlements = traffic %d device %d speed %d, want %d/%d/%d",
			item.TrafficBytes, item.DeviceLimit, item.SpeedLimitMbps,
			plan.TrafficBytes, plan.DeviceLimit, plan.SpeedLimitMbps)
	}
}
