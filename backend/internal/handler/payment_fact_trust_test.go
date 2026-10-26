package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func TestPaymentIntegrationRequiresCurrentAdministrator(t *testing.T) {
	assertPaymentIntegrationTrust(t, newOrderFixture(t))
}
func TestMySQLPaymentIntegrationRequiresCurrentAdministrator(t *testing.T) {
	assertPaymentIntegrationTrust(t, newMySQLOrderFixture(t))
}
func assertPaymentIntegrationTrust(t *testing.T, f orderFixture) {
	t.Helper()
	order := f.create(t, 0)
	issue := identity.IntegrationIssue{Name: "operator payment integration", Scopes: []string{"commerce.orders.list", "commerce.payments.record"}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	issued, err := f.h.services.Integrations().Issue(t.Context(), 1, issue)
	if err != nil {
		t.Fatal(err)
	}
	credential := catalog.Credential{Kind: "integration", Proof: issued.Token}
	grant, err := f.h.services.IntegrationAuthority().Resolve(t.Context(), credential, "commerce.payments.record")
	if err != nil {
		t.Fatal(err)
	}
	// Revoke the current role after resolution; an already resolved grant must
	// fail both admission and the final financial transaction.
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(issue)
	create := httptest.NewRecorder()
	f.h.IntegrationCredentialCreateHandler(create, announcementRequest("POST", "/api/v1/account/integrations/credentials", f.token, string(body)))
	if create.Code != http.StatusForbidden {
		t.Fatalf("self-issued payment authority: %d %s", create.Code, create.Body.String())
	}
	var tokens int64
	if err := f.h.db.Model(&model.UserAPIToken{}).Count(&tokens).Error; err != nil || tokens != 1 {
		t.Fatalf("denied request persisted a token: %d %v", tokens, err)
	}
	if _, err := f.h.services.IntegrationAuthority().Resolve(t.Context(), credential, "commerce.payments.record"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("legacy payment scope authorized ordinary user: %v", err)
	}
	if _, err := f.h.services.IntegrationAuthority().Resolve(t.Context(), credential, "commerce.orders.list"); err != nil {
		t.Fatalf("ordinary own-order read lost: %v", err)
	}
	if err := f.h.services.IntegrationAdmission().Admit(t.Context(), credential, grant, catalog.Descriptor{Name: "commerce.payments.record", RateLimitPerMinute: 120}); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("stale admin grant admitted: %v", err)
	}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "trust-event", ProviderTradeNo: "trust-trade", Status: "paid", AmountMinor: order.PayableAmount, Currency: order.Currency, OccurredAt: time.Now().UTC()}
	if _, err := (commercestore.PaymentFacts{DB: f.h.db}).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: fmt.Sprintf("integration:%d", issued.Credential.ID), RequireAdministrator: true}, fact); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("final transaction ignored revoked authority: %v", err)
	}
	raw, _ := json.Marshal(fact)
	invoke := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/integrations/capabilities/commerce.payments.record/invoke", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	f.h.IntegrationInvokeHandler(invoke, pathvar.WithVars(req, map[string]string{"name": "commerce.payments.record"}))
	if invoke.Code != http.StatusUnauthorized {
		t.Fatalf("ordinary fact accepted: %d %s", invoke.Code, invoke.Body.String())
	}
	for _, table := range []string{"payment_events", "payment_receipts", "subscriptions", "protocol_credentials", "node_config_publishes"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("denial changed %s: %d %v", table, count, err)
		}
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil || actual.Status != "pending" {
		t.Fatalf("denial fulfilled order: %+v %v", actual, err)
	}
}

func TestPaymentFactRollsBackAndReplaysWithDurablePublication(t *testing.T) {
	assertPaymentFactRecovery(t, newOrderFixture(t), false)
}
func TestMySQLPaymentFactRollsBackAndReplaysWithDurablePublication(t *testing.T) {
	assertPaymentFactRecovery(t, newMySQLBusinessOrderFixture(t), true)
}
func assertPaymentFactRecovery(t *testing.T, f orderFixture, mysql bool) {
	t.Helper()
	endpoint := attachOrderPublishEndpoint(t, f)
	order := f.create(t, 0)
	issued, err := f.h.services.Integrations().Issue(t.Context(), 1, identity.IntegrationIssue{Name: "operator reconciliation", Scopes: []string{"commerce.payments.record"}, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "recover-payment", ProviderTradeNo: "recover-trade", Status: "paid", AmountMinor: order.PayableAmount, Currency: order.Currency, OccurredAt: time.Now().UTC()}
	raw, _ := json.Marshal(fact)
	invoke := func() *httptest.ResponseRecorder {
		// Rebuild the registry on every attempt, as after an application restart;
		// replay safety must come from committed records rather than process memory.
		out := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/integrations/capabilities/commerce.payments.record/invoke", strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+issued.Token)
		f.h.IntegrationInvokeHandler(out, pathvar.WithVars(req, map[string]string{"name": "commerce.payments.record"}))
		return out
	}
	if mysql {
		if err := f.h.db.Exec(`CREATE TRIGGER reject_publish BEFORE INSERT ON node_config_publishes FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'publication unavailable'`).Error; err != nil {
			t.Fatal(err)
		}
	} else {
		rejectPublishInsert(t, f)
	}
	if out := invoke(); out.Code != http.StatusInternalServerError {
		t.Fatalf("outbox failure: %d %s", out.Code, out.Body.String())
	}
	for _, table := range []string{"payment_events", "payment_receipts", "subscriptions", "protocol_credentials", "quota_events", "node_config_publishes"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("failed transaction committed %s: %d %v", table, count, err)
		}
	}
	var pending model.Order
	if err := f.h.db.First(&pending, order.ID).Error; err != nil || pending.Status != "pending" || pending.FulfilledAt != nil {
		t.Fatalf("partial payment: %+v %v", pending, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_publish").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if out := invoke(); out.Code != http.StatusOK {
			t.Fatalf("replay %d: %d %s", i, out.Code, out.Body.String())
		}
	}
	for _, mutate := range []func(*commerce.PaymentFact){
		func(v *commerce.PaymentFact) { v.ProviderTradeNo = "changed-trade" },
		func(v *commerce.PaymentFact) { v.AmountMinor++ },
		func(v *commerce.PaymentFact) { v.Currency = "ZZZ" },
		func(v *commerce.PaymentFact) { v.OccurredAt = v.OccurredAt.Add(time.Second) },
	} {
		changed := fact
		mutate(&changed)
		raw, _ = json.Marshal(changed)
		if out := invoke(); out.Code != http.StatusConflict {
			t.Fatalf("changed replay accepted: %d %s", out.Code, out.Body.String())
		}
	}
	for _, table := range []string{"payment_events", "payment_receipts", "subscriptions", "protocol_credentials", "quota_events", "node_config_publishes"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("replay duplicated %s: %d %v", table, count, err)
		}
	}
	var queued model.NodeConfigPublish
	if err := f.h.db.First(&queued, endpoint.NodeID).Error; err != nil || queued.Generation != 1 || queued.Reconciling {
		t.Fatalf("undelivered intent: %+v %v", queued, err)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.First(&credential).Error; err != nil || credential.SubscriptionID == 0 {
		t.Fatalf("business authorization missing: %v", err)
	}
	var confirmed int64
	if err := f.h.db.Model(&model.ProtocolDeployment{}).Where("node_id = ? AND status = ?", endpoint.NodeID, "success").Count(&confirmed).Error; err != nil || confirmed != 0 {
		t.Fatalf("payment fabricated delivery confirmation: %d %v", confirmed, err)
	}
}

func TestMySQLPaymentFactRacesAdministratorConfirmation(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	order := f.create(t, 0)
	issued, err := f.h.services.Integrations().Issue(t.Context(), 1, identity.IntegrationIssue{Name: "race", Scopes: []string{"commerce.payments.record"}, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	fact, _ := json.Marshal(commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "race-fact", ProviderTradeNo: "race-trade", Status: "paid", AmountMinor: order.PayableAmount, Currency: order.Currency, OccurredAt: time.Now().UTC()})
	start := make(chan struct{})
	failures := make(chan string, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				if _, err := f.h.applyOrderResult(t.Context(), orderResultCommand{OrderID: order.ID, Status: orderStatusPaid, Actor: authClaims{UserID: 1, IsAdmin: true}}); err != nil {
					failures <- err.Error()
				}
				return
			}
			out := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/v1/integrations/capabilities/commerce.payments.record/invoke", strings.NewReader(string(fact)))
			req.Header.Set("Authorization", "Bearer "+issued.Token)
			f.h.IntegrationInvokeHandler(out, pathvar.WithVars(req, map[string]string{"name": "commerce.payments.record"}))
			if out.Code != http.StatusOK {
				failures <- fmt.Sprintf("%d %s", out.Code, out.Body.String())
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	for _, table := range []string{"subscriptions", "protocol_credentials", "payment_events", "quota_events"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("race %s: %d %v", table, count, err)
		}
	}
	var queued model.NodeConfigPublish
	if err := f.h.db.First(&queued, endpoint.NodeID).Error; err != nil || queued.Generation != 1 {
		t.Fatalf("duplicate race publication: %+v %v", queued, err)
	}
}
