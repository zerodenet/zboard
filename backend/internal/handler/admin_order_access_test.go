package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func orderAccess(t *testing.T, f orderFixture, id uint, token string) (*httptest.ResponseRecorder, subscriptionAccessView) {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.AdminOrderSubscriptionAccessHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/orders/%d/subscription-access", id), token, ""))
	var response struct{ Data subscriptionAccessView }
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	return w, response.Data
}

func TestAdminOrderAccessUsesFulfilledSubscriptionAndAuditsWithoutCredentials(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	_, order := assignOrder(t, f, assignmentRequest(f, buyer.ID))
	if _, v := orderAccess(t, f, order.ID, f.token); v.Configured {
		t.Fatal("pending order exposed a token")
	}
	paid := f.paid(t, order.ID)
	w, v := orderAccess(t, f, order.ID, f.token)
	if w.Code != 200 || !v.Configured || v.SubscriptionID != paid.SubscriptionID || v.SubscriptionURL == "" || v.Token != "" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response is cacheable")
	}
	_, again := orderAccess(t, f, order.ID, f.token)
	if again.SubscriptionURL != v.SubscriptionURL {
		t.Fatal("reading rotated the token")
	}
	var audit []model.AuditLog
	f.h.db.Where("action = ?", "order.subscription_access").Find(&audit)
	b, _ := json.Marshal(audit)
	if len(audit) != 2 || strings.Contains(string(b), strings.TrimPrefix(v.SubscriptionURL, "/api/v1/client/subscription/")) {
		t.Fatal("missing audit or leaked credential")
	}
	if w, _ := orderAccess(t, f, order.ID, ""); w.Code != 401 {
		t.Fatal("anonymous access", w.Code)
	}
	userToken, _, _ := f.h.issueToken(authClaims{UserID: buyer.ID, Email: buyer.Email})
	if w, _ := orderAccess(t, f, order.ID, userToken); w.Code != 403 {
		t.Fatal("non-admin access", w.Code)
	}
	if err := f.h.db.Model(&model.SubscriptionToken{}).Where("subscription_id = ?", paid.SubscriptionID).Update("revoked_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if w, v := orderAccess(t, f, order.ID, f.token); w.Code != 200 || v.Configured || v.SubscriptionURL != "" {
		t.Fatal("revoked token exposed", w.Code)
	}
}

func TestAdminOrderAccessRejectsMismatchedOwnerAndInactiveSubscription(t *testing.T) {
	for _, change := range []string{"owner", "expired", "disabled", "exhausted"} {
		t.Run(change, func(t *testing.T) {
			f := newOrderFixture(t)
			buyer := assignmentBuyer(t, f)
			_, order := assignOrder(t, f, assignmentRequest(f, buyer.ID))
			paid := f.paid(t, order.ID)
			var err error
			switch change {
			case "owner":
				err = f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("user_id", 1).Error
			case "expired":
				err = f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("end_at", time.Now().Add(-time.Hour)).Error
			case "disabled":
				err = f.h.db.Model(&model.User{}).Where("id = ?", buyer.ID).Update("status", "disabled").Error
			case "exhausted":
				err = f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("flow_used", paid.TrafficBytes).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			_, view := orderAccess(t, f, order.ID, f.token)
			if view.Configured || view.SubscriptionURL != "" {
				t.Fatal("unavailable credential returned")
			}
		})
	}
}

func TestAdminOrderAccessKeepsEachPurchasedSubscriptionSeparate(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	urls := map[string]bool{}
	ids := map[uint]bool{}
	for i := 0; i < 2; i++ {
		_, order := assignOrder(t, f, assignmentRequest(f, buyer.ID))
		paid := f.paid(t, order.ID)
		w, view := orderAccess(t, f, order.ID, f.token)
		if w.Code != 200 || !view.Configured || view.SubscriptionID != paid.SubscriptionID || urls[view.SubscriptionURL] || ids[view.SubscriptionID] {
			t.Fatalf("orders shared delivery credentials: %d %s", w.Code, w.Body.String())
		}
		urls[view.SubscriptionURL] = true
		ids[view.SubscriptionID] = true
	}
}

func TestAdminOrderAccessRollsBackProvisioningWhenAuditFails(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	_, order := assignOrder(t, f, assignmentRequest(f, buyer.ID))
	paid := f.paid(t, order.ID)
	if err := f.h.db.Exec(`CREATE TRIGGER fail_access_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	w, view := orderAccess(t, f, order.ID, f.token)
	if w.Code != 500 || view.SubscriptionURL != "" {
		t.Fatalf("audit failure exposed credentials: %d %s", w.Code, w.Body.String())
	}
	var count int64
	if err := f.h.db.Model(&model.SubscriptionToken{}).Where("subscription_id = ?", paid.SubscriptionID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("credential was not rolled back: count=%d err=%v", count, err)
	}
}
