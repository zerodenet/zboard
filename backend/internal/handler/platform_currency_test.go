package handler

import (
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"reflect"
	"testing"
)

func TestPlatformCurrencyChangePreservesFinancialFacts(t *testing.T) {
	f := newOrderFixture(t)
	fundTestWallet(t, f, 500)
	before, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	defaults := platformstore.SystemConfigDefaults{DB: f.h.db}
	if err := defaults.ReconcileSystemConfigDefaults(t.Context(), platform.DefaultSystemConfigs()); err != nil {
		t.Fatal(err)
	}
	var config model.SystemConfig
	if err := f.h.db.Where("config_key = ?", platform.DefaultCurrencyKey).First(&config).Error; err != nil {
		t.Fatal(err)
	}
	service := f.h.services.SettingUpdate(f.h.credentialCipher)
	view, err := service.Update(t.Context(), 1, platform.SettingUpdateInput{Key: platform.DefaultCurrencyKey, Value: json.RawMessage(`" usd "`), ExpectedRevision: &config.Revision})
	if err != nil || view.Value != "USD" || !view.IsPublic || view.Revision != config.Revision+1 {
		t.Fatal(view, err)
	}
	if value, err := platformstore.DefaultCurrency(f.h.db); err != nil || value != "USD" {
		t.Fatal(value, err)
	}
	if err := defaults.ReconcileSystemConfigDefaults(t.Context(), platform.DefaultSystemConfigs()); err != nil {
		t.Fatal(err)
	}
	if value, err := platformstore.DefaultCurrency(f.h.db); err != nil || value != "USD" {
		t.Fatal("operator value overwritten", value, err)
	}
	after, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("historical wallet changed", before, after, err)
	}
	walletMatches(t, f, 500, 0, 1)
	usd, err := f.h.services.Wallet.Read(t.Context(), 1, "USD", 0, 50)
	if err != nil || usd.BalanceMinor != 0 {
		t.Fatal("implicit conversion", usd, err)
	}
	var orders []model.Order
	if err := f.h.db.Find(&orders).Error; err != nil {
		t.Fatal(err)
	}
	for _, order := range orders {
		if order.Currency != "CNY" {
			t.Fatal("historical order relabelled", order.ID, order.Currency)
		}
	}
	_, err = service.Update(t.Context(), 1, platform.SettingUpdateInput{Key: platform.DefaultCurrencyKey, Value: json.RawMessage(`"EUR"`), ExpectedRevision: &config.Revision})
	if !errors.Is(err, platform.ErrSettingRevision) {
		t.Fatal("stale setting write accepted", err)
	}
	_, err = service.Update(t.Context(), 1, platform.SettingUpdateInput{Key: platform.DefaultCurrencyKey, Value: json.RawMessage(`"JPY"`)})
	var invalid *platform.SettingValidation
	if !errors.As(err, &invalid) {
		t.Fatal("unsupported precision accepted", err)
	}
	_, err = service.Update(t.Context(), 0, platform.SettingUpdateInput{Key: platform.DefaultCurrencyKey, Value: json.RawMessage(`"EUR"`)})
	if !errors.Is(err, platform.ErrSettingsPermission) {
		t.Fatal("anonymous update allowed", err)
	}
	var audits int64
	f.h.db.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "system.config.update", "system_config:"+platform.DefaultCurrencyKey).Count(&audits)
	if audits != 1 {
		t.Fatal("unexpected audit count", audits)
	}
}
