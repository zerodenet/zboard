package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type redemptionMemoryProvider struct {
	state, token         string
	calls                []string
	unknownReserve       bool
	unknownBeforeReserve bool
	afterQuery           func()
}

func (p *redemptionMemoryProvider) RedeemDiscount(_ context.Context, r commerce.DiscountRedemptionRequest) (commerce.DiscountRedemptionResult, error) {
	p.calls = append(p.calls, r.Action)
	if r.Action == "query" && p.afterQuery != nil {
		hook := p.afterQuery
		p.afterQuery = nil
		hook()
	}
	switch r.Action {
	case "reserve":
		if p.unknownBeforeReserve {
			p.unknownBeforeReserve = false
			return commerce.DiscountRedemptionResult{}, context.DeadlineExceeded
		}
		if p.state == "absent" {
			p.state = "reserved"
			p.token = "hold-one"
		}
		if p.unknownReserve {
			p.unknownReserve = false
			return commerce.DiscountRedemptionResult{}, context.DeadlineExceeded
		}
	case "commit":
		if p.state == "reserved" {
			p.state = "committed"
		}
	case "release":
		if p.state != "committed" {
			p.state = "released"
		}
	}
	return commerce.DiscountRedemptionResult{Version: 1, Key: r.Binding.Key, BindingHash: r.BindingHash, State: p.state, Guarantee: commerce.DiscountRedemptionGuarantee, Token: p.token}, nil
}

func redemptionStoreFixture(t *testing.T) (*gorm.DB, model.Order, commerce.PurchaseDiscountSnapshot, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "redemption.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.AutoMigrate(&model.OrderDiscountRedemption{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
	order := model.Order{ID: 1, UserID: 2, Status: "pending", AmountCents: 12000, DiscountAmount: 2000, PayableAmount: 10000, Currency: "CNY"}
	accepted := commerce.PurchaseDiscountSnapshot{FinalPriceMinor: 10000, Context: commerce.DiscountQuoteContext{BuyerID: 2, InputFingerprint: strings.Repeat("a", 64)}, Quote: commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: "example.quote", SigningKeyFingerprint: strings.Repeat("b", 64), PackageDigest: strings.Repeat("c", 64), Generation: 1, ConfigRevision: 1}, Offer: commerce.DiscountOffer{Status: "available", DiscountMinor: 2000, Agreement: &commerce.DiscountAgreement{ID: "finite", Version: "1", TermsHash: strings.Repeat("d", 64)}}}}
	if err := db.Transaction(func(tx *gorm.DB) error { return CreateDiscountRedemptionIntent(tx, order, accepted, now) }); err != nil {
		t.Fatal(err)
	}
	return db, order, accepted, &now
}

func TestDiscountRedemptionSQLiteLostReserveACKQueriesSameKeyAndCommitsAfterFinancialIntent(t *testing.T) {
	db, order, accepted, now := redemptionStoreFixture(t)
	p := &redemptionMemoryProvider{state: "absent", unknownReserve: true}
	jobs := DiscountRedemptionJobs{DB: db, Provider: p, Now: func() time.Time { return *now }}
	if err := RequireDiscountReservation(db, order, true, *now); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("unconfirmed gate: %v", err)
	}
	var before model.OrderDiscountRedemption
	if err := db.First(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := jobs.Step(context.Background(), order.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unknown: %v", err)
	}
	if err := RequireDiscountReservation(db, order, true, *now); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatal("lost ACK cannot authorize payment")
	}
	*now = now.Add(11 * time.Second)
	if err := jobs.Step(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if err := RequireDiscountReservation(db, order, true, *now); err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.calls, ",") != "query,reserve,query" {
		t.Fatalf("must query without duplicate reserve: %v", p.calls)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return CreateDiscountRedemptionIntent(tx, order, accepted, *now) }); err != nil {
		t.Fatal(err)
	}
	var after model.OrderDiscountRedemption
	if err := db.First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if before.RedemptionKey != after.RedemptionKey {
		t.Fatal("idempotent creation replaced unknown/confirmed key")
	}
	sentinel := errors.New("financial rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := CommitDiscountRedemptionIntent(tx, order.ID, *now); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := db.First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.Desired != "reserve" {
		t.Fatal("commit intent survived financial rollback")
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return CommitDiscountRedemptionIntent(tx, order.ID, *now) }); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(11 * time.Second)
	if err := jobs.Step(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if p.state != "committed" {
		t.Fatal("durable commit not executed")
	}
	order.Status = "paid"
	if err := ReleaseDiscountRedemptionIntent(db, order, *now); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
		t.Fatalf("paid release: %v", err)
	}
	if err := jobs.Step(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.calls, ",") != "query,reserve,query,query,commit" {
		t.Fatalf("repeated finalization: %v", p.calls)
	}
}

func TestDiscountRedemptionSQLiteCancellationDuringQueryWritesReleaseTombstone(t *testing.T) {
	db, order, _, now := redemptionStoreFixture(t)
	p := &redemptionMemoryProvider{state: "absent"}
	p.afterQuery = func() {
		order.Status = "canceled"
		if err := db.Transaction(func(tx *gorm.DB) error { return ReleaseDiscountRedemptionIntent(tx, order, *now) }); err != nil {
			t.Fatal(err)
		}
	}
	jobs := DiscountRedemptionJobs{DB: db, Provider: p, Now: func() time.Time { return *now }}
	if err := jobs.Step(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.calls, ",") != "query,release" || p.state != "released" {
		t.Fatalf("cancellation raced into reserve: %v %s", p.calls, p.state)
	}
	if err := RequireDiscountReservation(db, order, true, *now); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("released gate: %v", err)
	}
	// A delayed old Reserve can never allocate another unit after release.
	var row model.OrderDiscountRedemption
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	var b commerce.DiscountRedemptionBinding
	if err := json.Unmarshal([]byte(row.BindingJSON), &b); err != nil {
		t.Fatal(err)
	}
	out, err := p.RedeemDiscount(context.Background(), commerce.DiscountRedemptionRequest{Version: 1, Action: "reserve", Binding: b, BindingHash: row.BindingHash})
	if err != nil || out.State != "released" {
		t.Fatalf("tombstone lost: %+v %v", out, err)
	}
}

func TestDiscountRedemptionSQLiteUnknownBeforeAllocationCanRetrySameKeyOnlyAfterLinearizableAbsent(t *testing.T) {
	db, order, _, now := redemptionStoreFixture(t)
	p := &redemptionMemoryProvider{state: "absent", unknownBeforeReserve: true}
	jobs := DiscountRedemptionJobs{DB: db, Provider: p, Now: func() time.Time { return *now }}
	var first model.OrderDiscountRedemption
	if err := db.First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := jobs.Step(t.Context(), order.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	*now = now.Add(11 * time.Second)
	if err := jobs.Step(t.Context(), order.ID); err != nil {
		t.Fatal(err)
	}
	var after model.OrderDiscountRedemption
	if err := db.First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.RedemptionKey != first.RedemptionKey || after.Token != "hold-one" || p.state != "reserved" || strings.Join(p.calls, ",") != "query,reserve,query,reserve" {
		t.Fatalf("query-before-same-key retry failed: %+v %v", after, p.calls)
	}
}

func TestDiscountRedemptionSQLitePreviouslyConfirmedHoldLossCannotReserveAgain(t *testing.T) {
	db, order, _, now := redemptionStoreFixture(t)
	p := &redemptionMemoryProvider{state: "reserved", token: "hold-one"}
	jobs := DiscountRedemptionJobs{DB: db, Provider: p, Now: func() time.Time { return *now }}
	if err := jobs.Step(t.Context(), order.ID); err != nil {
		t.Fatal(err)
	}
	p.state = "absent"
	p.token = ""
	*now = now.Add(2 * time.Minute)
	if err := jobs.Step(t.Context(), order.ID); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
		t.Fatalf("lost confirmed hold re-reserved: %v", err)
	}
	*now = now.Add(11 * time.Second)
	if err := jobs.Step(t.Context(), order.ID); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
		t.Fatal(err)
	}
	if strings.Join(p.calls, ",") != "query,query,query" {
		t.Fatalf("disappeared hold triggered a mutation: %v", p.calls)
	}
	if err := RequireDiscountReservation(db, order, true, *now); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("lost hold can pay: %v", err)
	}
	var row model.OrderDiscountRedemption
	if err := db.First(&row).Error; err != nil || row.Token != "hold-one" || row.Observed != "conflict" {
		t.Fatalf("successful proof was erased: %+v %v", row, err)
	}
}

func TestDiscountRedemptionSQLiteHugeAuthorityIsNeverLoadedAndHealthyDispatcherProgresses(t *testing.T) {
	for _, field := range []string{"binding_json", "token"} {
		t.Run(field, func(t *testing.T) {
			db, order, accepted, now := redemptionStoreFixture(t)
			healthy := order
			healthy.ID = 2
			if err := CreateDiscountRedemptionIntent(db, healthy, accepted, *now); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TABLE order_fundings(order_id INTEGER PRIMARY KEY,state TEXT,expires_at DATETIME)").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", order.ID).Update(field, strings.Repeat("x", commerce.MaxDiscountRedemptionBytes+1)).Error; err != nil {
				t.Fatal(err)
			}
			fullLoads := 0
			const callback = "redemption-bounded-load"
			if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*model.OrderDiscountRedemption); ok {
					if id, ok := tx.Statement.Dest.(*model.OrderDiscountRedemption); ok && id.OrderID == 0 {
						fullLoads++
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			// All bad-order entrances reject on thin metadata, before a model body query.
			if err := RequireDiscountReservation(db, order, true, *now); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
				t.Fatal(err)
			}
			if err := CommitDiscountRedemptionIntent(db, order.ID, *now); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
				t.Fatal(err)
			}
			order.Status = "canceled"
			if err := ReleaseDiscountRedemptionIntent(db, order, *now); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
				t.Fatal(err)
			}
			jobs := DiscountRedemptionJobs{DB: db, Provider: &redemptionMemoryProvider{state: "absent"}, Now: func() time.Time { return *now }}
			if err := jobs.Step(t.Context(), order.ID); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
				t.Fatal(err)
			}
			if fullLoads != 0 {
				t.Fatalf("oversized authority loaded %d full bodies before rejection", fullLoads)
			}
			if err := db.Callback().Query().Remove(callback); err != nil {
				t.Fatal(err)
			}
			// Bad row was postponed, not deleted; a healthy row still confirms in this run.
			if _, err := jobs.RunBatch(t.Context(), 10); err != nil {
				t.Fatal(err)
			}
			var good model.OrderDiscountRedemption
			if err := db.First(&good, "order_id = ?", healthy.ID).Error; err != nil || good.Observed != "reserved" {
				t.Fatalf("healthy queue starved: %+v %v", good, err)
			}
			var retained int64
			if err := db.Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", order.ID).Count(&retained).Error; err != nil || retained != 1 {
				t.Fatal("bad evidence removed", retained, err)
			}
		})
	}
}

func TestDiscountRedemptionSQLiteExpiryOrphanDoesNotStarveHealthyCommit(t *testing.T) {
	db, order, accepted, now := redemptionStoreFixture(t)
	if err := db.Exec("CREATE TABLE orders(id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE order_fundings(order_id INTEGER PRIMARY KEY,state TEXT,expires_at DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", order.ID).Update("created_at", now.Add(-31*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	healthy := order
	healthy.ID = 2
	if err := CreateDiscountRedemptionIntent(db, healthy, accepted, *now); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", healthy.ID).Updates(map[string]any{"desired": "commit", "observed": "reserved", "token": "hold-one"}).Error; err != nil {
		t.Fatal(err)
	}
	provider := &redemptionMemoryProvider{state: "reserved", token: "hold-one"}
	jobs := DiscountRedemptionJobs{DB: db, Provider: provider, Now: func() time.Time { return *now }}
	if _, err := jobs.RunBatch(t.Context(), 10); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("orphan should be retained as explicit error: %v", err)
	}
	var good model.OrderDiscountRedemption
	if err := db.First(&good, "order_id = ?", healthy.ID).Error; err != nil || good.Observed != "committed" {
		t.Fatalf("orphan starved independent commit: %+v %v", good, err)
	}
	var orphan model.OrderDiscountRedemption
	if err := db.First(&orphan, "order_id = ?", order.ID).Error; err != nil || orphan.LastError != "expiry_reconciliation_required" || !orphan.NextAttemptAt.After(*now) {
		t.Fatalf("orphan evidence lost/busy loop: %+v %v", orphan, err)
	}
}
