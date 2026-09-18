package identitystore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestExternalCapabilityOwnsRegistrationAndDoesNotLinkByEmail(t *testing.T) {
	db, _ := administrationFixture(t)
	if err := db.Create(&model.Installation{ID: 1, SiteName: "test", AllowRegistration: true, InstalledAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.ExternalIdentities{Repository: ExternalIdentities{DB: db}, Tokens: identity.SessionTokens{Key: []byte("test")}, EmailCodes: identity.EmailCodes{Key: []byte("test")}}
	e := identity.ExternalEvidence{Authority: "oauth~one", Publisher: "publisher", Issuer: "https://identity.test", Subject: "subject-1", Email: "new@example.test", EmailVerified: true}
	ctx := context.Background()
	resolution, err := service.Resolve(ctx, e, identity.BindingConfirmation{})
	if err != nil || !resolution.RegistrationRequired {
		t.Fatal(resolution, err)
	}
	var count int64
	db.Model(&model.User{}).Count(&count)
	if count != 0 {
		t.Fatal("resolution registered prematurely")
	}
	completion := identity.ExternalCompletion{Authority: e.Authority, Publisher: e.Publisher, Evidence: &e}
	result, err := service.Finish(ctx, completion, identity.RegistrationEmail{})
	if err != nil || result.User.IsAdmin || result.Token == "" {
		t.Fatal(result, err)
	}
	var user model.User
	if err := db.First(&user, result.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.Password != "!external" || user.EmailVerifiedAt == nil {
		t.Fatal("incorrect core account defaults")
	}
	e.Subject = "other-subject"
	if _, err := service.Finish(ctx, completion, identity.RegistrationEmail{}); err == nil {
		t.Fatal("same email linked a different identity")
	}
	db.Model(&model.User{}).Count(&count)
	if count != 1 {
		t.Fatal(count)
	}
	e.Subject = "late-failure"
	e.Email = "rollback@example.test"
	service.Tokens.Key = nil
	if _, err := service.Finish(ctx, completion, identity.RegistrationEmail{}); !errors.Is(err, identity.ErrToken) {
		t.Fatal(err)
	}
	db.Model(&model.User{}).Where("email = ?", e.Email).Count(&count)
	if count != 0 {
		t.Fatal("failed session left an account")
	}
	db.Model(&model.ExternalIdentity{}).Where("id = ?", e.Key()).Count(&count)
	if count != 0 {
		t.Fatal("failed session left a binding")
	}
}

func TestExternalCapabilityRequiresConfirmedAccountAndExactAuthority(t *testing.T) {
	db, _ := administrationFixture(t)
	users := []model.User{{Email: "owner@example.test", Password: "confirmed-hash", Status: "active"}, {Email: "other@example.test", Password: "other-hash", Status: "active"}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.ExternalIdentities{Repository: ExternalIdentities{DB: db}, Tokens: identity.SessionTokens{Key: []byte("test")}}
	e := identity.ExternalEvidence{Authority: "oauth~one", Publisher: "publisher", Issuer: "https://identity.test", Subject: "subject-1"}
	ctx := context.Background()
	if _, err := service.Resolve(ctx, e, identity.BindingConfirmation{AccountID: users[0].ID, PasswordHash: "old-hash"}); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatal(err)
	}
	resolution, err := service.Resolve(ctx, e, identity.BindingConfirmation{AccountID: users[0].ID, PasswordHash: users[0].Password})
	if err != nil || !resolution.Linked {
		t.Fatal(resolution, err)
	}
	if _, err := service.Resolve(ctx, e, identity.BindingConfirmation{AccountID: users[1].ID, PasswordHash: users[1].Password}); !errors.Is(err, identity.ErrBindingConflict) {
		t.Fatal(err)
	}
	completion := identity.ExternalCompletion{Authority: "another-authority", Publisher: e.Publisher, UserID: users[0].ID, BindingID: resolution.BindingID}
	if _, err := service.Finish(ctx, completion, identity.RegistrationEmail{}); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatal(err)
	}
	completion.Authority = e.Authority
	if _, err := service.Finish(ctx, completion, identity.RegistrationEmail{}); err != nil {
		t.Fatal(err)
	}
}
