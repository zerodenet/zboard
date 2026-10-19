package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func TestOrdinaryAccountTopUpUsesSignedNativePaymentPlugin(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%v", canceled), func(t *testing.T) { assertOrdinaryNativeTopUp(t, canceled) })
	}
}
func assertOrdinaryNativeTopUp(t *testing.T, canceled bool) {
	f := newOrderFixture(t)
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	f.token = token
	order := createTopUp(t, f, 12000, "ordinary-topup-intent")
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteURL: "https://panel.example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	occurred := time.Now().UTC().Truncate(time.Second)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"paid": true, "event_id": "ordinary-topup-receipt", "trade_no": "ordinary-provider-trade", "occurred_at": occurred})
	}))
	defer provider.Close()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.NewManager(f.h.db, f.h.credentialCipher, plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"payment.tests": base64.StdEncoding.EncodeToString(public)}}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	host := application.NewPluginHostServices(f.h.services, f.h)
	f.h.ConfigureNativePluginHost(host)
	manager.SetHostServices(host)
	f.h.SetPluginManager(manager)
	installed, err := manager.Import(paymentPluginPackage(t, "example.wallet-topup", public, private), "admin")
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]string{"provider": provider.URL})
	if _, err := manager.SaveConfig(t.Context(), installed.ID, "admin", 0, config); err != nil {
		t.Fatal(err)
	}
	installed, err = manager.Action(t.Context(), installed.ID, "enable", "admin", installed.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.CreateSlotSession(installed.ID, "checkout", plugins.CheckoutPaymentSlot, "account", 1, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SessionCheckoutAction(t.Context(), session.Token, 1, false, order.ID, "mock.prepare", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if canceled {
		if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		result, err := manager.SessionCheckoutAction(t.Context(), session.Token, 1, false, order.ID, "mock.reconcile", json.RawMessage(`{}`))
		if err != nil || !strings.Contains(string(result), `"status":"paid"`) {
			t.Fatalf("native receipt: %s %v", result, err)
		}
	}
	wallet, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || wallet.BalanceMinor != 12000 || len(wallet.Entries) != 1 {
		t.Fatalf("native topup: %+v %v", wallet, err)
	}
	for _, table := range []string{"subscriptions", "protocol_credentials", "node_config_publishes"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("topup granted %s: %d %v", table, count, err)
		}
	}
	if _, err := manager.Action(t.Context(), installed.ID, "disable", "admin", installed.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SessionCheckoutAction(t.Context(), session.Token, 1, false, order.ID, "mock.reconcile", json.RawMessage(`{}`)); err == nil {
		t.Fatal("disabled native process retained payment authority")
	}
}
