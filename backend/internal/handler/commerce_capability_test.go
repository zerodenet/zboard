package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func TestCommerceCapabilitiesKeepPaymentInsidePluginAndSettlementAuthoritative(t *testing.T) {
	f := newOrderFixture(t)
	order := f.create(t, 0)
	issued, err := f.h.services.Integrations().Issue(t.Context(), 1, identity.IntegrationIssue{
		Name: "payment plugin backend", Scopes: []string{"commerce.orders.list", "commerce.payments.record"}, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(name, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/capabilities/"+name+"/invoke", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+issued.Token)
		f.h.IntegrationInvokeHandler(recorder, pathvar.WithVars(request, map[string]string{"name": name}))
		return recorder
	}
	listed := invoke("commerce.orders.list", `{"status":"pending","limit":10}`)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), order.TradeNo) {
		t.Fatalf("orders list: %d %s", listed.Code, listed.Body.String())
	}
	fact := fmt.Sprintf(`{"order_id":%d,"provider_event_id":"evt-1","provider_trade_no":"provider-1","status":"paid","amount_minor":%d,"currency":%q,"occurred_at":%q}`, order.ID, order.PayableAmount, order.Currency, time.Now().UTC().Format(time.RFC3339))
	first := invoke("commerce.payments.record", fact)
	if first.Code != http.StatusOK {
		t.Fatalf("record: %d %s", first.Code, first.Body.String())
	}
	second := invoke("commerce.payments.record", fact)
	if second.Code != http.StatusOK {
		t.Fatalf("idempotent record: %d %s", second.Code, second.Body.String())
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Status != "paid" || actual.SubscriptionID == 0 || actual.Channel != fmt.Sprintf("integration:%d", issued.Credential.ID) || actual.ProviderTradeNo == nil || *actual.ProviderTradeNo != "provider-1" {
		t.Fatalf("settled order: %+v", actual)
	}
	for table, want := range map[string]int64{"payment_events": 1, "subscriptions": 1} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var response struct {
		Data struct {
			Fulfilled bool `json:"fulfilled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil || !response.Data.Fulfilled {
		t.Fatalf("first fulfillment response: %s err=%v", first.Body.String(), err)
	}
	tampered := strings.Replace(fact, fmt.Sprintf(`"amount_minor":%d`, order.PayableAmount), fmt.Sprintf(`"amount_minor":%d`, order.PayableAmount+1), 1)
	conflict := invoke("commerce.payments.record", tampered)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"conflict"`) {
		t.Fatalf("tampered fact: %d %s", conflict.Code, conflict.Body.String())
	}
}
