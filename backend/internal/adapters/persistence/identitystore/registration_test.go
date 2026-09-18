package identitystore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestLocalRegistrationOwnsPolicyVerificationAndAtomicCommit(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Create(&model.Installation{ID: 1, SiteName: "test", InstalledAt: now, AllowRegistration: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("config_key = ?", "register_email_verification").Assign(model.SystemConfig{Value: "true"}).FirstOrCreate(&model.SystemConfig{ConfigKey: "register_email_verification", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.Registration{Repository: Registration{DB: db}, Tokens: identity.SessionTokens{Key: []byte("test"), Now: func() time.Time { return now }}, EmailCodes: identity.EmailCodes{Key: []byte("test")}}
	in := identity.RegistrationInput{Email: " Owner@Example.Test ", Password: "ValidPassword2026!", Code: "123456"}
	challenge := model.RegistrationEmailChallenge{Email: "owner@example.test", Purpose: identity.RegistrationPurpose, CodeHash: service.EmailCodes.Digest(in.Email, in.Code), ExpiresAt: now.Add(time.Minute), LastSentAt: now}
	if err := db.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wrong := in
	wrong.Code = "654321"
	for attempt := 1; attempt <= identity.RegistrationAttemptLimit; attempt++ {
		if _, err := service.Register(ctx, wrong); err == nil {
			t.Fatal("wrong code accepted")
		}
		if err := db.First(&challenge, challenge.ID).Error; err != nil {
			t.Fatal(err)
		}
		if challenge.Attempts != attempt {
			t.Fatal("failed attempt not committed", challenge.Attempts, attempt)
		}
		if attempt < identity.RegistrationAttemptLimit && !challenge.ExpiresAt.After(now) {
			t.Fatal("budget expired too early")
		}
	}
	if _, err := service.Register(ctx, in); err == nil {
		t.Fatal("exhausted challenge accepted")
	}
	if err := db.Model(&challenge).Updates(map[string]any{"attempts": 0, "expires_at": now.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	service.Tokens.Key = nil
	if _, err := service.Register(ctx, in); !errors.Is(err, identity.ErrToken) {
		t.Fatal(err)
	}
	assertRegistrationRolledBack(t, db, challenge.ID)
	service.Tokens.Key = []byte("test")
	if err := db.Callback().Create().Before("gorm:create").Register("test:reject_registration_audit", func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "account.register" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(ctx, in); err == nil {
		t.Fatal("audit failure accepted")
	}
	assertRegistrationRolledBack(t, db, challenge.ID)
	if err := db.Callback().Create().Remove("test:reject_registration_audit"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Installation{}).Where("id = ?", 1).Update("allow_registration", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(ctx, in); !errors.Is(err, identity.ErrRegistrationClosed) {
		t.Fatal(err)
	}
	if err := db.Model(&model.Installation{}).Where("id = ?", 1).Update("allow_registration", true).Error; err != nil {
		t.Fatal(err)
	}
	result, err := service.Register(ctx, in)
	if err != nil || result.Token == "" || result.User.Email != "owner@example.test" || result.User.IsAdmin {
		t.Fatal(result, err)
	}
	var user model.User
	if err := db.First(&user, result.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.Status != "active" || user.EmailVerifiedAt == nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(in.Password)) != nil {
		t.Fatal("invalid local account")
	}
	if err := db.First(&challenge, challenge.ID).Error; err != nil || challenge.ConsumedAt == nil {
		t.Fatal("challenge not consumed", err)
	}
	if _, err := service.Register(ctx, in); err == nil {
		t.Fatal("challenge replay accepted")
	}
}

func assertRegistrationRolledBack(t *testing.T, db *gorm.DB, id uint) {
	t.Helper()
	var n int64
	if err := db.Model(&model.User{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("account escaped rollback", n, err)
	}
	var challenge model.RegistrationEmailChallenge
	if err := db.First(&challenge, id).Error; err != nil || challenge.ConsumedAt != nil {
		t.Fatal("challenge escaped rollback", err)
	}
}

func TestLocalRegistrationWithoutChallengeKeepsDeletedEmailReserved(t *testing.T) {
	db, _ := administrationFixture(t)
	if err := db.Create(&model.Installation{ID: 1, SiteName: "test", InstalledAt: time.Now().UTC(), AllowRegistration: true}).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.Registration{Repository: Registration{DB: db}, Tokens: identity.SessionTokens{Key: []byte("test")}}
	in := identity.RegistrationInput{Email: "local@example.test", Password: "ValidPassword2026!"}
	out, err := service.Register(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var user model.User
	if err := db.First(&user, out.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.EmailVerifiedAt != nil {
		t.Fatal("unverified email marked verified")
	}
	if err := db.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(context.Background(), in); !errors.Is(err, identity.ErrEmailConflict) {
		t.Fatal(err)
	}
}

func TestLocalRegistrationConcurrentRequestsConsumeChallengeOnce(t *testing.T) {
	a, b := administrationFixture(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := a.Create(&model.Installation{ID: 1, SiteName: "test", InstalledAt: now, AllowRegistration: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.Where("config_key = ?", "register_email_verification").Assign(model.SystemConfig{Value: "true"}).FirstOrCreate(&model.SystemConfig{ConfigKey: "register_email_verification", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	codes := identity.EmailCodes{Key: []byte("test")}
	in := identity.RegistrationInput{Email: "parallel@example.test", Password: "ValidPassword2026!", Code: "123456"}
	challenge := model.RegistrationEmailChallenge{Email: in.Email, Purpose: identity.RegistrationPurpose, CodeHash: codes.Digest(in.Email, in.Code), ExpiresAt: now.Add(time.Minute), LastSentAt: now}
	if err := a.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for _, db := range []*gorm.DB{a, b} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			service := identity.Registration{Repository: Registration{DB: db}, Tokens: identity.SessionTokens{Key: []byte("test")}, EmailCodes: codes}
			_, err := service.Register(context.Background(), in)
			errs <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("expected one completed registration", success)
	}
	var count int64
	if err := a.Model(&model.User{}).Where("email = ?", in.Email).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := a.Model(&model.AuditLog{}).Where("action = ?", "account.register").Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := a.First(&challenge, challenge.ID).Error; err != nil || challenge.ConsumedAt == nil {
		t.Fatal("missing consumption", err)
	}
}
