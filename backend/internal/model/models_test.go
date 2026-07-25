package model

import (
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestPlanSKUIDUsesMigratedColumnName(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "subscription", value: &Subscription{}},
		{name: "order", value: &Order{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := schema.Parse(tt.value, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatalf("parse model schema: %v", err)
			}

			field := parsed.LookUpField("PlanSKUID")
			if field == nil {
				t.Fatal("PlanSKUID field is missing")
			}
			if field.DBName != "plan_sku_id" {
				t.Fatalf("PlanSKUID database column = %q, want %q", field.DBName, "plan_sku_id")
			}
		})
	}
}

func TestSubscriptionTokenKeepsOptionalSubscriptionForeignKeyNull(t *testing.T) {
	parsed, err := schema.Parse(&SubscriptionToken{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := parsed.LookUpField("SubscriptionID")
	if field == nil {
		t.Fatal("SubscriptionID field is missing")
	}
	if field.DBName != "subscription_id" {
		t.Fatalf("SubscriptionID database column = %q, want %q", field.DBName, "subscription_id")
	}
	if field.FieldType.Kind() != reflect.Ptr {
		t.Fatalf("SubscriptionID kind = %s, want pointer so an unbound user token persists as NULL", field.FieldType.Kind())
	}
	if field.NotNull {
		t.Fatal("SubscriptionID must remain nullable")
	}
}

func TestProtocolCredentialPrincipalAndSubscriptionAreStableColumns(t *testing.T) {
	parsed, err := schema.Parse(&ProtocolCredential{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for fieldName, columnName := range map[string]string{
		"SubscriptionID":     "subscription_id",
		"ProtocolEndpointID": "protocol_endpoint_id",
		"CredentialID":       "credential_id",
		"PrincipalKey":       "principal_key",
	} {
		field := parsed.LookUpField(fieldName)
		if field == nil || field.DBName != columnName {
			t.Fatalf("%s database column = %v, want %q", fieldName, field, columnName)
		}
	}
}
