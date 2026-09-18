package identitystore

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestBindingRemovalChecksCurrentAuthorityAndRollsBackWithAudit(t *testing.T) {
	db, _ := administrationFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("local-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	users := []model.User{
		{Email: "owner@example.test", Password: string(hash), Status: "active"},
		{Email: "admin@example.test", Password: string(hash), Status: "active", IsAdmin: true},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.ExternalIdentity{ID: "binding-one", UserID: users[0].ID, PluginID: "oauth~one", Publisher: "publisher", Issuer: "issuer", Subject: "subject"}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.BindingRemoval{Repository: BindingRemoval{DB: db}}
	ctx := context.Background()
	base := identity.UnlinkRequest{ActorID: users[0].ID, TargetID: users[0].ID, PluginID: "oauth", BindingID: binding.ID, Password: "local-password"}
	for _, tc := range []struct {
		name   string
		change func(*identity.UnlinkRequest)
		want   error
	}{
		{"wrong password", func(in *identity.UnlinkRequest) { in.Password = "wrong" }, identity.ErrPassword},
		{"other plugin", func(in *identity.UnlinkRequest) { in.PluginID = "oaut" }, identity.ErrPermission},
		{"claimed admin", func(in *identity.UnlinkRequest) { in.Administrative = true }, identity.ErrPermission},
		{"cross account", func(in *identity.UnlinkRequest) { in.ActorID = users[1].ID }, identity.ErrPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.change(&in)
			if err := service.Unlink(ctx, in); !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
	// Inject a persistence failure on both supported databases, after deletion.
	if err := db.Callback().Create().Before("gorm:create").Register("test:reject_unlink_audit", func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "plugin.identity.unlink" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Unlink(ctx, base); err == nil {
		t.Fatal("expected audit failure")
	}
	if err := db.Where("id = ?", binding.ID).First(&model.ExternalIdentity{}).Error; err != nil {
		t.Fatal("binding lost on rollback", err)
	}
	if err := db.Callback().Create().Remove("test:reject_unlink_audit"); err != nil {
		t.Fatal(err)
	}
	// The current database role, not a caller's administrative flag, is authoritative.
	admin := base
	admin.ActorID = users[1].ID
	admin.Administrative = true
	admin.Password = ""
	if err := db.Model(&users[1]).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Unlink(ctx, admin); !errors.Is(err, identity.ErrPermission) {
		t.Fatal(err)
	}
	if err := db.Model(&users[1]).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&users[0]).Update("status", "suspended").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Unlink(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.ExternalIdentity{}).Where("id = ?", binding.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var audit model.AuditLog
	if err := db.Where("action = ?", "plugin.identity.unlink").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Actor != users[1].Email || audit.UserID == nil || *audit.UserID != users[1].ID {
		t.Fatal("incorrect audit actor", audit)
	}
}
