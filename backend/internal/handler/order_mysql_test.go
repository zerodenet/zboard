package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func newMySQLOrderFixture(t *testing.T) orderFixture {
	t.Helper()
	h, _ := newMySQLPublishHandlers(t)
	user := model.User{ID: 1, Email: "reader@example.test", Password: "unused", Status: userStatusActive}
	if err := h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "Catalog", Code: "catalog", IsEnabled: true}
	if err := h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	return newOrderFixtureFromCatalog(t, catalogFixture{h: h, group: group})
}

func TestMySQLOrderSettlementPublicationAndExpiry(t *testing.T) {
	f := newMySQLOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	order := f.create(t, 0)
	if err := f.h.db.Exec(`CREATE TRIGGER reject_publish BEFORE INSERT ON node_config_publishes
 FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'publication persistence unavailable'`).Error; err != nil {
		t.Fatal(err)
	}
	if w := f.pay(t, order.ID, false); w.Code != http.StatusInternalServerError {
		t.Fatalf("queue failure: %d %s", w.Code, w.Body.String())
	}
	var stored model.Order
	if err := f.h.db.First(&stored, order.ID).Error; err != nil || stored.Status != orderStatusPending || stored.SubscriptionID != 0 {
		t.Fatalf("partial settlement: %+v %v", stored, err)
	}
	for _, table := range []string{"subscriptions", "protocol_credentials", "quota_events", "audit_logs", "node_config_publishes"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s committed: %d %v", table, count, err)
		}
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_publish").Error; err != nil {
		t.Fatal(err)
	}
	// Concurrent confirmations must grant once under InnoDB row locking.
	start := make(chan struct{})
	type outcome struct {
		result orderResult
		err    error
	}
	results := make(chan outcome, 8)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			result, err := f.h.applyOrderResult(context.Background(), orderResultCommand{
				OrderID: order.ID, Status: orderStatusPaid, Actor: authClaims{UserID: 1, IsAdmin: true},
			})
			results <- outcome{result, err}
		}()
	}
	close(start)
	fulfilled := 0
	for i := 0; i < 8; i++ {
		result := <-results
		if result.err != nil {
			t.Errorf("confirmation: %v", result.err)
		}
		if result.result.Fulfilled {
			fulfilled++
		}
	}
	if t.Failed() {
		return
	}
	if fulfilled != 1 {
		t.Fatalf("granted %d times", fulfilled)
	}
	var pending model.NodeConfigPublish
	if err := f.h.db.First(&pending, endpoint.NodeID).Error; err != nil || pending.Generation != 1 {
		t.Fatalf("duplicate publication: %+v %v", pending, err)
	}
	if err := f.h.db.First(&stored, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", stored.SubscriptionID).Update("end_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec(`CREATE TRIGGER reject_expiry_publish BEFORE INSERT ON node_config_publishes
 FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'publication persistence unavailable'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := expireSubscriptions(f.h.db, 1, time.Now().UTC()); err == nil {
		t.Fatal("expiry ignored queue failure")
	}
	var subscription model.Subscription
	if err := f.h.db.First(&subscription, stored.SubscriptionID).Error; err != nil || subscription.Status != subStatusActive {
		t.Fatalf("expiry partially committed: %+v %v", subscription, err)
	}
	if err := f.h.db.First(&pending, endpoint.NodeID).Error; err != nil || pending.Generation != 1 {
		t.Fatalf("rolled back expiry changed queue: %+v %v", pending, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_expiry_publish").Error; err != nil {
		t.Fatal(err)
	}
	if err := expireSubscriptions(f.h.db, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&pending, endpoint.NodeID).Error; err != nil || pending.Generation != 2 {
		t.Fatalf("expiry publication: %+v %v", pending, err)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", stored.SubscriptionID).First(&credential).Error; err != nil || credential.Status != "expired" {
		t.Fatalf("credential still usable: %+v %v", credential, err)
	}
}

func TestMySQLConcurrentSettlementRespectsPlanCapacity(t *testing.T) {
	f := newMySQLOrderFixture(t)
	if err := f.h.db.Model(&f.planRecord).Update("max_active_subscriptions", 1).Error; err != nil {
		t.Fatal(err)
	}
	first := f.create(t, 0)
	user := model.User{Email: "second@example.test", Password: "unused", Status: userStatusActive}
	if err := f.h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: user.ID, Email: user.Email})
	if err != nil {
		t.Fatal(err)
	}
	secondFixture := f
	secondFixture.token = token
	second := secondFixture.create(t, 0)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []uint{first.ID, second.ID} {
		go func(id uint) {
			<-start
			_, err := f.h.applyOrderResult(context.Background(), orderResultCommand{OrderID: id, Status: orderStatusPaid, Actor: authClaims{UserID: 1, IsAdmin: true}})
			results <- err
		}(id)
	}
	close(start)
	paid, limited := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			paid++
		case errors.Is(err, errPlanSubscriptionLimitReached):
			limited++
		default:
			t.Errorf("settlement failed unexpectedly: %v", err)
		}
	}
	if paid != 1 || limited != 1 {
		t.Fatalf("capacity raced: paid=%d limited=%d", paid, limited)
	}
	var count int64
	if err := f.h.db.Model(&model.Subscription{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("capacity exceeded: %d %v", count, err)
	}
}
