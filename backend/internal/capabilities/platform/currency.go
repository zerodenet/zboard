package platform

import "fmt"

const DefaultCurrencyKey = "default_currency"
const FallbackCurrency = "CNY"

// These ISO 4217 currencies use two minor-unit digits, matching the existing
// price and wallet accounting contract. Source: SIX List One (2026-11-05).
func DefaultCurrencyOptions() []SettingInputOption {
	return []SettingInputOption{
		{Label: "人民币 · CNY", Value: "CNY"},
		{Label: "美元 · USD", Value: "USD"},
		{Label: "欧元 · EUR", Value: "EUR"},
		{Label: "英镑 · GBP", Value: "GBP"},
		{Label: "港币 · HKD", Value: "HKD"},
		{Label: "新加坡元 · SGD", Value: "SGD"},
		{Label: "澳元 · AUD", Value: "AUD"},
		{Label: "加拿大元 · CAD", Value: "CAD"},
		{Label: "瑞士法郎 · CHF", Value: "CHF"},
		{Label: "新西兰元 · NZD", Value: "NZD"},
	}
}

func ValidateDefaultCurrency(value string) error {
	for _, option := range DefaultCurrencyOptions() {
		if value == option.Value {
			return nil
		}
	}
	return fmt.Errorf("请选择支持的默认币种。")
}
