package platform

import "testing"

func TestSubscriptionAlertDefaultsAndValidation(t *testing.T) {
	for _, entry := range subscriptionAlertDefaults() {
		if entry.Public || entry.Secret {
			t.Fatal("alert settings must be private admin settings")
		}
		if err := ValidateSettingValue(entry.Key, entry.Value); err != nil {
			t.Fatal(err)
		}
		if entry.ValueType == "bool" && entry.Value != "false" {
			t.Fatal("automatic email enabled by default")
		}
		schema := SettingInputSchemaFor(Setting{ConfigKey: entry.Key, ValueType: entry.ValueType})
		if entry.ValueType == "int" && (schema.Min == nil || schema.Max == nil) {
			t.Fatal("missing UI bounds")
		}
	}
	for key, values := range map[string][]string{
		"subscription_alert_remaining_percent": {"0", "91", "1.5"},
		"subscription_alert_expiring_days":     {"0", "31"},
		"subscription_alert_interval_hours":    {"0", "5", "169"},
		"subscription_alert_low_enabled":       {"yes", "1"},
	} {
		for _, value := range values {
			if ValidateSettingValue(key, value) == nil {
				t.Fatalf("accepted %s=%s", key, value)
			}
		}
	}
}
