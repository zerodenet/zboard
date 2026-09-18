package identitystore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type testCodeDelivery func(context.Context, string, string) error

func (f testCodeDelivery) SendRegistrationCode(ctx context.Context, email, code string) error {
	return f(ctx, email, code)
}

func TestCodeIssuancePolicyCooldownAndFailedDeliveryRecovery(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Create(&model.Installation{ID: 1, SiteName: "test", InstalledAt: now, AllowRegistration: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("config_key = ?", "register_email_verification").Assign(model.SystemConfig{Value: "true"}).FirstOrCreate(&model.SystemConfig{ConfigKey: "register_email_verification", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	delivered := 0
	service := identity.CodeIssuance{Repository: CodeIssuance{DB: db}, Codes: identity.EmailCodes{Key: []byte("test")}, Now: func() time.Time { return now }, Delivery: testCodeDelivery(func(ctx context.Context, email, code string) error {
		if email != "new@example.test" || !identity.ValidEmailCode(code) {
			t.Fatal("invalid delivery")
		}
		delivered++
		return nil
	})}
	ctx := context.Background()
	in := identity.CodeRequest{Email: " New@Example.Test ", SourceHash: "source"}
	if err := service.Send(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := service.Send(ctx, in); !errors.Is(err, identity.ErrCodeCooldown) || delivered != 1 {
		t.Fatal(err, delivered)
	}
	now = now.Add(identity.CodeCooldown)
	request, cancel := context.WithCancel(ctx)
	service.Delivery = testCodeDelivery(func(context.Context, string, string) error { cancel(); return errors.New("SMTP failed") })
	if err := service.Send(request, in); !errors.Is(err, identity.ErrCodeDelivery) {
		t.Fatal(err)
	}
	var row model.RegistrationEmailChallenge
	if err := db.Where("email = ?", "new@example.test").First(&row).Error; err != nil || row.CodeHash != "" || row.ExpiresAt.After(now) {
		t.Fatal("failure did not invalidate", err)
	}
	service.Delivery = testCodeDelivery(func(context.Context, string, string) error { return nil })
	if err := service.Send(ctx, in); err != nil {
		t.Fatal("retry after delivery failure", err)
	}
	if err := db.Model(&model.Installation{}).Where("id = ?", 1).Update("allow_registration", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Send(ctx, in); !errors.Is(err, identity.ErrRegistrationClosed) {
		t.Fatal(err)
	}
}

func TestCodeIssuanceLateFailureCannotInvalidateReplacement(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Create(&model.Installation{ID: 1, SiteName: "test", InstalledAt: now, AllowRegistration: true}).Error; err != nil {
		t.Fatal(err)
	}
	repo := CodeIssuance{DB: db}
	codes := identity.EmailCodes{Key: []byte("test")}
	old := identity.IssuedCode{Email: "new@example.test", Hash: codes.Digest("new@example.test", "111111"), SourceHash: "source", SentAt: now, ExpiresAt: now.Add(identity.CodeLifetime)}
	newer := old
	newer.Hash = codes.Digest(old.Email, "222222")
	newer.SentAt = now.Add(time.Minute)
	newer.ExpiresAt = newer.SentAt.Add(identity.CodeLifetime)
	for _, value := range []identity.IssuedCode{old, newer} {
		if err := repo.WithinCodeIssuance(context.Background(), func(tx identity.CodeIssuanceTx) error { return tx.StoreCode(value) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.InvalidateCode(context.Background(), old, now); err != nil {
		t.Fatal(err)
	}
	var row model.RegistrationEmailChallenge
	if err := db.Where("email = ?", old.Email).First(&row).Error; err != nil || row.CodeHash != newer.Hash {
		t.Fatal("new code revoked", err)
	}
}

func TestCodeIssuanceSourceAddressBudgetAndEmailOwnership(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Now().UTC()
	if err := db.Create(&model.Installation{ID: 1, SiteName: "test", InstalledAt: now, AllowRegistration: true}).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.CodeIssuance{Repository: CodeIssuance{DB: db}, Codes: identity.EmailCodes{Key: []byte("test")}, Delivery: testCodeDelivery(func(context.Context, string, string) error { return nil })}
	for i := 0; i < identity.CodeSourceAddressLimit; i++ {
		if err := service.Send(context.Background(), identity.CodeRequest{Email: fmt.Sprintf("u%d@example.test", i), SourceHash: "source", External: true}); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := service.Send(context.Background(), identity.CodeRequest{Email: "extra@example.test", SourceHash: "source", External: true}); !errors.Is(err, identity.ErrCodeRateLimited) {
		t.Fatal(err)
	}
	owner := model.User{Email: "owner@example.test", Password: "!external", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Send(context.Background(), identity.CodeRequest{Email: owner.Email, SourceHash: "another", External: true}); !errors.Is(err, identity.ErrEmailConflict) {
		t.Fatal(err)
	}
}
