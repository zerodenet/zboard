package model

import (
	"testing"

	"gorm.io/gorm"
)

func TestValidateSystemTimezone(t *testing.T) {
	for _, value := range []string{"UTC", "Asia/Shanghai", "America/Los_Angeles"} {
		if err := validateSystemTimezone(value); err != nil {
			t.Fatalf("validateSystemTimezone(%q): %v", value, err)
		}
	}
	for _, value := range []string{"", "UTC+8", "Not/A_Real_Zone"} {
		if err := validateSystemTimezone(value); err == nil {
			t.Fatalf("validateSystemTimezone(%q) unexpectedly succeeded", value)
		}
	}
}

func TestSystemConfigUpdateValueUsesPendingValue(t *testing.T) {
	tx := &gorm.DB{Statement: &gorm.Statement{Dest: map[string]interface{}{"value": "Asia/Shanghai"}}}
	if got := systemConfigUpdateValue(tx, "UTC"); got != "Asia/Shanghai" {
		t.Fatalf("systemConfigUpdateValue() = %q, want Asia/Shanghai", got)
	}
}
