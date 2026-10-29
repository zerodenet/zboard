package platform

import "testing"

func TestRecoveryWindowAdministrativeValidationAndInput(t *testing.T) {
	for _, value := range []string{"0", "7", "14", "3650"} {
		if err := ValidateSettingValue("subscription_renewal_grace_days", value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"-1", "3651", "1.5", "unknown", "999999999999999999"} {
		if ValidateSettingValue("subscription_renewal_grace_days", value) == nil {
			t.Fatal("unsafe recovery policy accepted", value)
		}
	}
	schema := SettingInputSchemaFor(Setting{ConfigKey: "subscription_renewal_grace_days", ValueType: "int"})
	if schema.Control != "integer" || schema.Min == nil || *schema.Min != 0 || schema.Max == nil || *schema.Max != 3650 {
		t.Fatal("admin input disagrees with authority", schema)
	}
	for _, row := range DefaultSystemConfigs() {
		if row.Key == "subscription_renewal_grace_days" {
			t.Fatal("generic defaults must not recreate an initialized, lost recovery policy")
		}
	}
}
