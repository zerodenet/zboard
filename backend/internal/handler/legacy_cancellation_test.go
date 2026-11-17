package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestLegacyCancellationUsesWholeTerminationWithoutRefund(t *testing.T) {
	assertLegacyCancellation(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLLegacyCancellationUsesWholeTerminationWithoutRefund(t *testing.T) {
	assertLegacyCancellation(t, newMySQLBusinessOrderFixture)
}
func assertLegacyCancellation(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			f := fixture(t)
			attachOrderPublishEndpoint(t, f)
			priceForFunding(t, f, 1000)
			fundTestWallet(t, f, 4000)
			root := f.create(t, 0)
			p := fundingService(f)
			if _, err := p.Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			if _, err := p.Pay(t.Context(), 1, root.ID); err != nil {
				t.Fatal(err)
			}
			fact := readValueEvidence(t, f, root.ID)
			other := f.paid(t, f.create(t, 0).ID)
			pending := f.create(t, fact.SubscriptionID)
			if _, err := p.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			if unknown {
				if err := f.h.db.Model(&model.Order{}).Where("id = ?", root.ID).Update("entitlement_value_snapshot", "").Error; err != nil {
					t.Fatal(err)
				}
			}
			admin := model.User{Email: "legacy-cancel-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
			ordinary := model.User{Email: "legacy-cancel-ordinary@example.test", Password: "unused", Status: "active"}
			for _, u := range []*model.User{&admin, &ordinary} {
				if err := f.h.db.Create(u).Error; err != nil {
					t.Fatal(err)
				}
			}
			token := model.SubscriptionToken{UserID: 1, SubscriptionID: &fact.SubscriptionID, TokenHash: strings.Repeat("c", 64), TokenPrefix: "cancel-zero"}
			consent := model.SubscriptionAutomaticRenewal{SubscriptionID: fact.SubscriptionID, UserID: 1, Enabled: true, PlanSKUID: f.skuRecord.ID, RootOrderID: root.ID, AgreementDigest: strings.Repeat("c", 64), State: "enabled", NextAttemptAt: time.Now().UTC()}
			if err := f.h.db.Create(&token).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Create(&consent).Error; err != nil {
				t.Fatal(err)
			}
			q, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), admin.ID, fact.SubscriptionID)
			if err != nil {
				t.Fatal(err)
			}
			if unknown && q.TotalMinor != nil || !unknown && (q.TotalMinor == nil || *q.TotalMinor <= 0) {
				t.Fatalf("default value %+v", q)
			}
			in := entitlements.SubscriptionMutationInput{SubscriptionID: fact.SubscriptionID, Kind: entitlements.SubscriptionCancel, Reason: "operator stops all remaining rights without refund", IdempotencyKey: "plugin:" + strings.Repeat("d", 64), Origin: "plugin:legacy.cancel"}
			mutations := f.h.services.SubscriptionMutations()
			if _, err := mutations.Apply(t.Context(), ordinary.ID, in); !errors.Is(err, entitlements.ErrAdministrativeRead) {
				t.Fatalf("ordinary command %v", err)
			}
			// The old persistence adapter cannot remain a second status-only writer.
			old := entitlementstore.SubscriptionMutations{DB: f.h.db, Publish: networkstore.EnqueueSubscriptionPublications}
			if _, err := old.Apply(t.Context(), admin.ID, in); !errors.Is(err, entitlements.ErrSubscriptionMutationUnavailable) {
				t.Fatalf("second cancellation writer %v", err)
			}
			var before model.NodeConfigPublish
			if err := f.h.db.First(&before).Error; err != nil {
				t.Fatal(err)
			}
			trigger := `CREATE TRIGGER reject_legacy_cancel_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'subscription.status.cancel' BEGIN SELECT RAISE(ABORT, 'legacy audit unavailable'); END`
			if !datastore.IsSQLite(f.h.db) {
				trigger = `CREATE TRIGGER reject_legacy_cancel_audit BEFORE INSERT ON audit_logs FOR EACH ROW BEGIN IF NEW.action = 'subscription.status.cancel' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'legacy audit unavailable'; END IF; END`
			}
			if err := f.h.db.Exec(trigger).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := mutations.Apply(t.Context(), admin.ID, in); err == nil {
				t.Fatal("unaudited legacy whole stop committed")
			}
			var sub model.Subscription
			if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil || sub.Status != "active" || sub.EndedAt != nil {
				t.Fatalf("partial stop %+v %v", sub, err)
			}
			walletMatches(t, f, 3000, 1000, 4)
			var after model.NodeConfigPublish
			if err := f.h.db.First(&after, before.NodeID).Error; err != nil || after.Generation != before.Generation {
				t.Fatalf("partial publication %+v %v", after, err)
			}
			var n int64
			if err := f.h.db.Model(&model.SubscriptionMutation{}).Count(&n).Error; err != nil || n != 0 {
				t.Fatalf("partial receipt %d %v", n, err)
			}
			if err := f.h.db.Exec("DROP TRIGGER reject_legacy_cancel_audit").Error; err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			receipts := make(chan entitlements.SubscriptionMutationReceipt, 8)
			errs := make(chan error, 8)
			for range 8 {
				wg.Add(1)
				go func() { defer wg.Done(); r, e := mutations.Apply(t.Context(), admin.ID, in); receipts <- r; errs <- e }()
			}
			wg.Wait()
			close(receipts)
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("concurrent legacy cancellation %v", err)
				}
			}
			var id uint
			for r := range receipts {
				if r.MutationID == 0 || id != 0 && id != r.MutationID || r.SubscriptionID != fact.SubscriptionID || r.Status != "canceled" {
					t.Fatalf("receipt %+v", r)
				}
				id = r.MutationID
			}
			walletMatches(t, f, 3000, 0, 5)
			if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil || sub.EndReason != "canceled" {
				t.Fatalf("whole rights %+v %v", sub, err)
			}
			if err := f.h.db.First(&token, token.ID).Error; err != nil || token.RevokedAt == nil {
				t.Fatalf("token %+v %v", token, err)
			}
			if err := f.h.db.First(&consent, fact.SubscriptionID).Error; err != nil || consent.Enabled {
				t.Fatalf("automatic renewal %+v %v", consent, err)
			}
			if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status <> ?", sub.ID, "revoked").Count(&n).Error; err != nil || n != 0 {
				t.Fatalf("live credentials %d %v", n, err)
			}
			if err := f.h.db.First(&after, before.NodeID).Error; err != nil || after.Generation <= before.Generation {
				t.Fatalf("missing durable intent %+v %v", after, err)
			}
			var otherSub model.Subscription
			if err := f.h.db.First(&otherSub, other.SubscriptionID).Error; err != nil || otherSub.Status != "active" {
				t.Fatalf("other subscription %+v %v", otherSub, err)
			}
			var paid model.Order
			if err := f.h.db.First(&paid, root.ID).Error; err != nil || paid.Status != "paid" || paid.PaidAmount != 1000 || paid.RefundAmount != 0 || paid.SubscriptionEndedAt == nil {
				t.Fatalf("payment history %+v %v", paid, err)
			}
			var funding model.OrderFunding
			if err := f.h.db.First(&funding, pending.ID).Error; err != nil || funding.State != "released" {
				t.Fatalf("pending hold %+v %v", funding, err)
			}
			for _, table := range []string{"subscription_value_settlements", "subscription_value_settlement_sources", "order_value_budgets"} {
				if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
					t.Fatalf("invented finance %s %d %v", table, n, err)
				}
			}
			var event model.QuotaEvent
			if err := f.h.db.Where("subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?", sub.ID, "unsubscribe", "subscription_mutation", strconv.FormatUint(uint64(id), 10)).First(&event).Error; err != nil {
				t.Fatal(err)
			}
			var evidence struct {
				ActorID       uint   `json:"actor_id"`
				ActualMinor   string `json:"actual_minor"`
				Reason        string
				CommandOrigin string `json:"command_origin"`
				Quote         commerce.AdministrativeSubscriptionValueQuote
			}
			if err := json.Unmarshal([]byte(event.Detail), &evidence); err != nil || evidence.ActorID != admin.ID || evidence.ActualMinor != "0" || evidence.Reason != in.Reason || evidence.CommandOrigin != in.Origin || evidence.Quote.ValuationStatus != q.ValuationStatus || (evidence.Quote.TotalMinor == nil) != (q.TotalMinor == nil) {
				t.Fatalf("approval evidence %s %v", event.Detail, err)
			}
			if q.TotalMinor != nil && *evidence.Quote.TotalMinor > *q.TotalMinor {
				t.Fatal("default residual increased during cancellation")
			}
			changed := in
			changed.Reason = "another command"
			if _, err := mutations.Apply(t.Context(), admin.ID, changed); !errors.Is(err, entitlements.ErrSubscriptionMutationConflict) {
				t.Fatalf("changed replay %v", err)
			}
			if err := f.h.db.Model(&event).Update("detail", "{}").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := mutations.Apply(t.Context(), admin.ID, in); !errors.Is(err, entitlements.ErrSubscriptionMutationConflict) {
				t.Fatalf("missing new receipt evidence %v", err)
			}
			if err := f.h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := mutations.Apply(t.Context(), admin.ID, in); !errors.Is(err, entitlements.ErrAdministrativeRead) {
				t.Fatalf("revoked administrator replay %v", err)
			}
		})
	}
}
