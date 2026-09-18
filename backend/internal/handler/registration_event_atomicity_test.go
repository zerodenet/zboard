package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/identitystore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRegistrationEventFailureRollsBackBothAccountPaths(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	failure := errors.New("fixture registration event failure")
	const callback = "registration-event-failure"
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "account_registration_events" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.db.Callback().Create().Remove(callback)
	ctx := context.Background()
	local := identitystore.Registration{DB: h.db}
	err := local.WithinRegistration(ctx, func(tx identity.RegistrationTx) error {
		_, err := tx.CreateLocalAccount("local-event@example.test", "hash", nil)
		return err
	})
	if !errors.Is(err, failure) {
		t.Fatal("local event write not reached", err)
	}
	external := identitystore.ExternalIdentities{DB: h.db}
	err = external.WithinExternal(ctx, func(tx identity.ExternalTx) error {
		_, err := tx.CreateExternalAccount("external-event@example.test", time.Now())
		return err
	})
	if !errors.Is(err, failure) {
		t.Fatal("external event write not reached", err)
	}
	var count int64
	if err := h.db.Model(&model.User{}).Where("email IN ?", []string{"local-event@example.test", "external-event@example.test"}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("account escaped failed event transaction", count, err)
	}
}
