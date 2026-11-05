package platform

import (
	"encoding/json"
	"testing"
)

func TestDefaultCurrencyConfigContract(t *testing.T) {
	var definition SystemConfigDefault
	for _, row := range DefaultSystemConfigs() {
		if row.Key == DefaultCurrencyKey {
			definition = row
		}
	}
	if definition.Value != "CNY" || !definition.Public || definition.Secret || definition.ValueType != "string" {
		t.Fatal(definition)
	}
	for _, option := range DefaultCurrencyOptions() {
		if err := ValidateSettingValue(DefaultCurrencyKey, option.Value); err != nil {
			t.Fatal(option, err)
		}
	}
	for _, value := range []string{"", "¥", "USDT", "JPY", "KWD", "ZZZ", "usd"} {
		if err := ValidateSettingValue(DefaultCurrencyKey, value); err == nil {
			t.Fatal("unsupported currency accepted", value)
		}
	}
	value, err := NormalizeSettingValue(Setting{ConfigKey: DefaultCurrencyKey, ValueType: "string"}, json.RawMessage(`" usd "`))
	if err != nil || value != "USD" {
		t.Fatal(value, err)
	}
	schema := SettingInputSchemaFor(Setting{ConfigKey: DefaultCurrencyKey, ValueType: "string"})
	if schema.Control != "select" || !schema.Required || len(schema.Options) != len(DefaultCurrencyOptions()) {
		t.Fatal(schema)
	}
}
