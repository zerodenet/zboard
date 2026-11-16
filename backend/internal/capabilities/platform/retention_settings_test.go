package platform

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"strconv"
	"testing"
)

func TestRetentionSettingsDefaultsBoundsAndSecretSafety(t *testing.T) {
	for _, p := range observability.RetentionSettings() {
		for _, value := range []int{0, p.Default, p.Maximum} {
			if e := ValidateSettingValue(p.Key, strconv.Itoa(value)); e != nil {
				t.Fatal(p.Key, e)
			}
		}
		for _, value := range []string{"-1", strconv.Itoa(p.Maximum + 1), "1.5", "garbage"} {
			if ValidateSettingValue(p.Key, value) == nil {
				t.Fatal("invalid policy accepted", p.Key, value)
			}
		}
		v, e := SettingToView(Setting{ConfigKey: p.Key, Value: "0", ValueType: "int"})
		if e != nil || v.DefaultValue == nil || *v.DefaultValue != p.Default || v.Input.Max == nil || *v.Input.Max != int64(p.Maximum) {
			t.Fatal(p.Key, v, e)
		}
		v, e = SettingToView(Setting{ConfigKey: p.Key, Value: "0", ValueType: "int", IsSecret: true})
		if e != nil || v.DefaultValue != nil || v.Value != nil {
			t.Fatal("secret default leaked", v, e)
		}
	}
}
