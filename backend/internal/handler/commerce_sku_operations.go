package handler

import (
	"errors"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	skuBillingPeriodic         = "periodic"
	skuBillingOneTime          = "one_time"
	skuEntitlementPlan         = "plan"
	skuEntitlementTrafficAddon = "traffic_addon"
	skuRenewalNone             = "none"
	skuRenewalExtendOnly       = "extend_only"
	skuRenewalExtendAndAdd     = "extend_and_add_quota"
	skuRenewalAddQuotaOnly     = "add_quota_only"

	skuOperationPurchase = "purchase"
	skuOperationRenew    = "renew"
	skuOperationChange   = "change"
	skuOperationAddon    = "addon"
)

type commercePlanSKURequest = commerce.SKURequest

type commercePlanCreateRequest = commerce.PlanCreateRequest

type commerceOrderCreateRequest = commerce.OrderCreateRequest

type normalizedCommerceSKU struct {
	SKU               model.PlanSKU
	BillingMode       string
	EntitlementMode   string
	RenewalEffect     string
	AllowedOperations []string
}

type commercePlanSKUItem = commerce.SKUView

func normalizeCommercePlanSKU(planID uint, request commercePlanSKURequest) (normalizedCommerceSKU, error) {
	value, err := commerce.NormalizeSKU(planID, request)
	if err != nil {
		return normalizedCommerceSKU{}, commerceValidationError(err)
	}
	return normalizedCommerceSKU{SKU: model.PlanSKU(value.SKU), BillingMode: value.BillingMode, EntitlementMode: value.EntitlementMode, RenewalEffect: value.RenewalEffect, AllowedOperations: value.AllowedOperations}, nil
}
func containsSKUOperation(operations []string, target string) bool {
	return commerce.ContainsOperation(operations, target)
}
func commerceValidationError(err error) error {
	var invalid *commerce.ValidationError
	if errors.As(err, &invalid) {
		return validationError(invalid.Message, invalid.Fields)
	}
	return err
}

func parseStrictBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, errors.New("invalid boolean")
	}
}
