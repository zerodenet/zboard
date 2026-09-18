package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestCommerceOrderCreationAuthorityAndRollback(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var f orderFixture
			if engine == "mysql" {
				f = newMySQLOrderFixture(t)
			} else {
				f = newOrderFixture(t)
			}
			service := f.h.services.OrderCreation
			request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID}
			ctx := context.Background()
			for _, buyer := range []uint{0, 999999} {
				out, err := service.Create(ctx, buyer, request)
				if !errors.Is(err, commerce.ErrBuyerUnavailable) || out.ID != 0 {
					t.Fatalf("unauthorized buyer %d: %+v %v", buyer, out, err)
				}
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if out, err := service.Create(canceled, 1, request); !errors.Is(err, context.Canceled) || out.ID != 0 {
				t.Fatalf("cancel: %+v %v", out, err)
			}
			other := assignmentBuyer(t, f)
			sub := model.Subscription{UserID: other.ID, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: time.Now(), EndAt: time.Now().Add(time.Hour), FlowTotal: 1024, Config: "{}"}
			if err := f.h.db.Create(&sub).Error; err != nil {
				t.Fatal(err)
			}
			targetReq := request
			targetReq.TargetSubscriptionID = sub.ID
			var invalid *commerce.ValidationError
			if out, err := service.Create(ctx, 1, targetReq); !errors.As(err, &invalid) || invalid.Fields["target_subscription_id"] == "" || out.ID != 0 {
				t.Fatalf("cross buyer target: %+v %v", out, err)
			}
			const callback = "order-create-rollback"
			if err := f.h.db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "orders" {
					tx.AddError(errors.New("injected after order insert"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			failed, err := service.Create(ctx, 1, request)
			f.h.db.Callback().Create().Remove(callback)
			if err == nil || failed.ID != 0 {
				t.Fatalf("failed transaction returned order: %+v %v", failed, err)
			}
			var count int64
			if err := f.h.db.Model(&model.Order{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("rollback: count=%d %v", count, err)
			}
			// Pending orders do not consume capacity. Existing active entitlements do.
			if err := f.h.db.Model(&f.planRecord).Update("max_active_subscriptions", 1).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.Create(ctx, 1, request); !errors.Is(err, commerce.ErrSubscriptionCapacity) {
				t.Fatalf("capacity: %v", err)
			}
			if err := f.h.db.Model(&sub).Update("flow_used", sub.FlowTotal).Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				out, err := service.Create(ctx, 1, request)
				if err != nil || out.UserID != 1 || out.AmountCents != 100 || out.TrafficBytes != 1024 || out.Status != "pending" {
					t.Fatalf("pending snapshot: %+v %v", out, err)
				}
			}
			if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("status", "disabled").Error; err != nil {
				t.Fatal(err)
			}
			if out, err := service.Create(ctx, 1, request); !errors.Is(err, commerce.ErrBuyerUnavailable) || out.ID != 0 {
				t.Fatalf("revoked buyer: %+v %v", out, err)
			}
		})
	}
}

func TestMySQLCommerceOrderCreationReadsCommittedSKUAfterParentLock(t *testing.T) {
	for _, mutation := range []string{"price", "disabled", "operation"} {
		t.Run(mutation, func(t *testing.T) {
			f := newMySQLOrderFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			writer := f.h.db.WithContext(ctx).Begin()
			if writer.Error != nil {
				t.Fatal(writer.Error)
			}
			defer writer.Rollback()
			var plan model.Plan
			if err := writer.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, f.planRecord.ID).Error; err != nil {
				t.Fatal(err)
			}
			identityRead := make(chan struct{})
			resume := make(chan struct{})
			const callback = "order-create-after-identity"
			if err := f.h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "plans" {
					select {
					case identityRead <- struct{}{}:
					case <-ctx.Done():
						tx.AddError(ctx.Err())
						return
					}
					select {
					case <-resume:
					case <-ctx.Done():
						tx.AddError(ctx.Err())
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer f.h.db.Callback().Query().Remove(callback)
			type result struct {
				order commerce.Order
				err   error
			}
			done := make(chan result, 1)
			go func() {
				out, err := f.h.services.OrderCreation.Create(ctx, 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID})
				done <- result{out, err}
			}()
			select {
			case <-identityRead:
			case <-ctx.Done():
				t.Fatal("order did not reach parent lock")
			}
			var err error
			switch mutation {
			case "price":
				err = writer.Model(&model.PlanSKU{}).Where("id = ?", f.skuRecord.ID).Update("price_cents", 777).Error
			case "disabled":
				err = writer.Model(&model.PlanSKU{}).Where("id = ?", f.skuRecord.ID).Update("is_active", false).Error
			case "operation":
				err = writer.Where("plan_sku_id = ? AND operation = ?", f.skuRecord.ID, "purchase").Delete(&model.PlanSKUOperation{}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.Commit().Error; err != nil {
				t.Fatal(err)
			}
			close(resume)
			var got result
			select {
			case got = <-done:
			case <-ctx.Done():
				t.Fatal("order did not finish")
			}
			if mutation == "price" {
				if got.err != nil || got.order.AmountCents != 777 || got.order.PayableAmount != 777 {
					t.Fatalf("stale price snapshot: %+v %v", got.order, got.err)
				}
			} else {
				var invalid *commerce.ValidationError
				if !errors.As(got.err, &invalid) || invalid.Fields["plan_sku_id"] == "" || got.order.ID != 0 {
					t.Fatalf("stale sale availability: %+v %v", got.order, got.err)
				}
			}
		})
	}
}
