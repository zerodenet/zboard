package handler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func TestSignedPaymentPluginsUseHostOrdersAndIdempotentSettlement(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(fmt.Sprintf("administrator=%t", admin), func(t *testing.T) { exerciseSignedPaymentPlugins(t, admin) })
	}
}

func exerciseSignedPaymentPlugins(t *testing.T, admin bool) {
	f := newOrderFixture(t)
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", admin).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: admin})
	if err != nil {
		t.Fatal(err)
	}
	f.token = token
	endpoint := attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteURL: "https://panel.example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	paid := atomic.Bool{}
	occurred := time.Now().UTC().Truncate(time.Second)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"paid": paid.Load(), "event_id": "event-stable-1", "trade_no": "provider-stable-1", "occurred_at": occurred})
	}))
	defer provider.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	options := plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"payment.tests": base64.StdEncoding.EncodeToString(public)}}
	newManager := func() *plugins.Manager {
		manager, err := plugins.NewManager(f.h.db, f.h.credentialCipher, options, "v0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		host := application.NewPluginHostServices(f.h.services, f.h)
		f.h.ConfigureNativePluginHost(host)
		manager.SetHostServices(host)
		f.h.SetPluginManager(manager)
		t.Cleanup(manager.Close)
		return manager
	}
	manager := newManager()
	installations := []plugins.Installation{}
	for _, id := range []string{"example.payment-one", "example.payment-two"} {
		installation, err := manager.Import(paymentPluginPackage(t, id, public, private), "admin")
		if err != nil {
			t.Fatal(err)
		}
		config, _ := json.Marshal(map[string]string{"provider": provider.URL})
		if _, err := manager.SaveConfig(t.Context(), id, "admin", 0, config); err != nil {
			t.Fatal(err)
		}
		installation, err = manager.Action(t.Context(), id, "enable", "admin", installation.Generation, false, "")
		if err != nil {
			t.Fatal(err)
		}
		installations = append(installations, installation)
	}
	slots, err := manager.Slots("account", plugins.CheckoutPaymentSlot, 1, admin)
	if err != nil || len(slots) != 2 {
		t.Fatalf("discovery: %+v %v", slots, err)
	}
	pages, err := manager.Pages("account", 1, admin)
	if err != nil || len(pages) != 0 {
		t.Fatalf("payment plugins added business pages: %+v %v", pages, err)
	}
	session, err := manager.CreateSlotSession(installations[0].ID, "checkout", plugins.CheckoutPaymentSlot, "account", 1, admin, 1)
	if err != nil {
		t.Fatal(err)
	}
	order := f.create(t, 0)
	action := func(s plugins.Session, kind string, payload any) json.RawMessage {
		body, _ := json.Marshal(payload)
		result, err := manager.SessionCheckoutAction(t.Context(), s.Token, 1, admin, order.ID, kind, body)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		return result
	}
	bridge := func(s plugins.Session, body string) *httptest.ResponseRecorder {
		request := announcementRequest("POST", "/api/v1/plugin-ui/bridge", f.token, body)
		request.Header.Set("X-Plugin-Session", s.Token)
		out := httptest.NewRecorder()
		f.h.PluginBridgeHandler(out, request)
		return out
	}
	prepared := action(session, "mock.prepare", map[string]any{"amount_minor": 1, "currency": "FAKE"})
	if !strings.Contains(string(prepared), order.TradeNo) || !strings.Contains(string(prepared), `"currency_precision":2`) || !strings.Contains(string(prepared), "https://panel.example.test/account/orders?") {
		t.Fatalf("trusted context missing: %s", prepared)
	}
	for _, body := range []string{`{"type":"checkout.call","order_id":1,"action":"mock.prepare","payload":{},"actor_id":"999"}`, `{"type":"checkout.call","order_id":1,"action":"mock.prepare","payload":{},"plugin_id":"example.payment-two"}`, `{"type":"checkout.call","order_id":1,"action":"mock.prepare","payload":{},"slot_id":"order"}`} {
		if res := bridge(session, body); res.Code != 400 {
			t.Fatalf("spoof fields accepted: %d %s", res.Code, res.Body.String())
		}
	}
	if _, err := manager.SessionCheckoutAction(t.Context(), session.Token, 2, false, order.ID, "mock.prepare", json.RawMessage(`{}`)); err == nil {
		t.Fatal("cross actor accepted")
	}
	if res := bridge(session, `{"type":"capabilities.invoke","operation":"commerce.payments.record","input":{}}`); res.Code != 403 {
		t.Fatalf("browser wrote facts: %d %s", res.Code, res.Body.String())
	}
	other := assignmentBuyer(t, f)
	otherOrder, err := f.h.services.OrderCreation.Create(t.Context(), other.ID, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result := action(session, "mock.read", map[string]any{"id": otherOrder.ID}); !strings.Contains(string(result), `"error":"forbidden"`) {
		t.Fatalf("cross account read: %s", result)
	}
	if result := action(session, "mock.read", map[string]any{"principal_id": "999"}); !strings.Contains(string(result), "error") {
		t.Fatalf("forged native principal: %s", result)
	}
	fact := map[string]any{"order_id": order.ID, "provider_event_id": "event-stable-1", "provider_trade_no": "provider-stable-1", "status": "paid", "amount_minor": order.PayableAmount, "currency": order.Currency, "occurred_at": occurred}
	bad := func(field string, value any) {
		clone := map[string]any{}
		for k, v := range fact {
			clone[k] = v
		}
		clone[field] = value
		raw := action(session, "mock.fact", clone)
		if !strings.Contains(string(raw), "error") {
			t.Fatalf("invalid %s accepted: %s", field, raw)
		}
	}
	bad("amount_minor", order.PayableAmount+1)
	bad("currency", "ZZZ")
	bad("order_id", otherOrder.ID)
	var count int64
	f.h.db.Model(&model.PaymentEvent{}).Count(&count)
	if count != 0 {
		t.Fatalf("denials wrote events: %d", count)
	}
	pending := action(session, "mock.reconcile", map[string]any{"paid": true})
	if !strings.Contains(string(pending), `"status":"pending"`) {
		t.Fatalf("unverified paid: %s", pending)
	}
	f.h.db.Model(&model.Subscription{}).Count(&count)
	if count != 0 {
		t.Fatal("unverified browser return fulfilled order")
	}
	paid.Store(true)
	orderSession, err := manager.CreateSlotSession(installations[0].ID, "order", plugins.OrderPaymentSlot, "account", 1, admin, 1)
	if err != nil {
		t.Fatal(err)
	}
	notify := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/.well-known/"+installations[0].ID+"/notification", strings.NewReader(fmt.Sprintf(`{"order_id":%d,"account_id":999}`, order.ID)))
		out := httptest.NewRecorder()
		f.h.PluginPublicRouteHandler(out, request)
		return out
	}
	var wg sync.WaitGroup
	failures := make(chan string, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 2 {
				res := notify()
				if res.Code != 200 {
					failures <- res.Body.String()
				}
				return
			}
			s := session
			if i == 1 {
				s = orderSession
			}
			raw, err := manager.SessionCheckoutAction(context.Background(), s.Token, 1, admin, order.ID, "mock.reconcile", json.RawMessage(`{}`))
			if err != nil || !strings.Contains(string(raw), `"status":"paid"`) {
				failures <- fmt.Sprintf("%s %v", raw, err)
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	checkSettled := func() {
		t.Helper()
		var stored model.Order
		if err := f.h.db.First(&stored, order.ID).Error; err != nil || stored.Status != "paid" || stored.SubscriptionID == 0 || stored.FulfilledAt == nil {
			t.Fatalf("settled order: %+v %v", stored, err)
		}
		for table, want := range map[string]int64{"payment_events": 1, "subscriptions": 1, "protocol_credentials": 1} {
			var n int64
			if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != want {
				t.Fatalf("%s=%d want %d err %v", table, n, want, err)
			}
		}
		var subscription model.Subscription
		if err := f.h.db.First(&subscription, stored.SubscriptionID).Error; err != nil || subscription.FlowTotal != 1024 || subscription.Status != "active" || subscription.NodeGroupID != f.group.ID {
			t.Fatalf("entitlements: %+v %v", subscription, err)
		}
		var publication model.NodeConfigPublish
		if err := f.h.db.First(&publication, endpoint.NodeID).Error; err != nil || publication.Generation != 1 {
			t.Fatalf("publication repeated: %+v %v", publication, err)
		}
		t.Logf("SDK settlement: order=%d status=%s subscription=%d provider=%s", stored.ID, stored.Status, stored.SubscriptionID, stored.Channel)
	}
	checkSettled()
	competing, err := manager.CreateSlotSession(installations[1].ID, "checkout", plugins.CheckoutPaymentSlot, "account", 1, admin, 1)
	if err != nil {
		t.Fatal(err)
	}
	if raw := action(competing, "mock.fact", fact); !strings.Contains(string(raw), `"error":"conflict"`) {
		t.Fatalf("competing provider fact: %s", raw)
	}
	bad("amount_minor", order.PayableAmount+2)
	bad("provider_trade_no", "changed")
	manager.RevokeSession(session.Token, 1)
	manager.RevokeSession(orderSession.Token, 1)
	if res := notify(); res.Code != 200 {
		t.Fatalf("background after browser close: %d %s", res.Code, res.Body.String())
	}
	manager.Close()
	manager = newManager()
	if res := notify(); res.Code != 200 {
		t.Fatalf("replay after process restart: %d %s", res.Code, res.Body.String())
	}
	checkSettled()
	// An unknown association cannot acquire authority from a public numeric account.
	unknown := httptest.NewRequest("POST", "/.well-known/"+installations[0].ID+"/notification", strings.NewReader(fmt.Sprintf(`{"order_id":%d,"account_id":%d}`, otherOrder.ID, other.ID)))
	denied := httptest.NewRecorder()
	f.h.PluginPublicRouteHandler(denied, unknown)
	if denied.Code != 403 {
		t.Fatalf("public arbitrary authority: %d %s", denied.Code, denied.Body.String())
	}
	all, err := manager.List()
	current := plugins.Installation{}
	for _, item := range all {
		if item.ID == installations[0].ID {
			current = item
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Action(t.Context(), current.ID, "disable", "admin", current.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	slots, err = manager.Slots("account", plugins.CheckoutPaymentSlot, 1, admin)
	if err != nil || len(slots) != 1 || slots[0].PluginID != installations[1].ID {
		t.Fatalf("disable isolation: %+v %v", slots, err)
	}
	if res := notify(); res.Code == 200 {
		t.Fatal("disabled process accepted payment")
	}
	if err := f.h.db.Where("plugin_id = ?", installations[1].ID).Delete(&model.PluginAuthorization{}).Error; err != nil {
		t.Fatal(err)
	}
	slots, err = manager.Slots("account", plugins.CheckoutPaymentSlot, 1, admin)
	if err != nil || len(slots) != 0 {
		t.Fatalf("revoked contribution remained: %+v %v", slots, err)
	}
	all, err = manager.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range all {
		if item.ID == installations[0].ID {
			if _, err := manager.Action(t.Context(), item.ID, "uninstall", "admin", item.Generation, false, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if res := notify(); res.Code == 200 {
		t.Fatal("uninstalled plugin accepted payment")
	}
	checkSettled()
}

func TestOwnedOrderCreationRecoversLostResponsesWithoutDuplicatingOrders(t *testing.T) {
	f := newOrderFixture(t)
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "host-checkout-intent-01"}
	first, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	// Price/availability changes must not prevent recovering the original result.
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"is_active": false, "price_cents": 999}).Error; err != nil {
		t.Fatal(err)
	}
	second, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil || second.ID != first.ID || second.PayableAmount != first.PayableAmount {
		t.Fatalf("recovery: %+v %v", second, err)
	}
	changed := request
	changed.Channel = "changed"
	if _, err := f.h.services.OrderCreation.Create(t.Context(), 1, changed); err == nil {
		t.Fatal("same intent accepted changed business request")
	}
	var n int64
	f.h.db.Model(&model.Order{}).Count(&n)
	if n != 1 {
		t.Fatalf("orders=%d", n)
	}
}
