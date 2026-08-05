package model

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestPlanSKUOperationUsesPlanSKUIDColumn(t *testing.T) {
	parsed, err := schema.Parse(&PlanSKUOperation{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := parsed.LookUpField("PlanSKUID")
	if field == nil {
		t.Fatal("PlanSKUID field was not parsed")
	}
	if field.DBName != "plan_sku_id" {
		t.Fatalf("PlanSKUID column = %q, want plan_sku_id", field.DBName)
	}
}
