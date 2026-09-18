package identitystore

import (
	"context"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRelationshipDirectoryProjectsAccountAndBindingsWithoutAuthentication(t *testing.T) {
	db, _ := administrationFixture(t)
	user := model.User{Email: "member@example.test", AccountName: "member", Password: "hash", Status: "disabled"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	bindings := []model.ExternalIdentity{
		{ID: "binding-new", UserID: user.ID, PluginID: "oauth~new", Publisher: "publisher", Issuer: "issuer-new", Subject: "subject-new"},
		{ID: "binding-old", UserID: user.ID, PluginID: "oauth~old", Publisher: "publisher", Issuer: "issuer-old", Subject: "subject-old"},
	}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.RelationshipDirectory{Repository: RelationshipDirectory{DB: db}}
	account, err := service.Account(context.Background(), user.ID)
	if err != nil || account.Status != "disabled" || account.Email != user.Email {
		t.Fatalf("account=%+v error=%v", account, err)
	}
	rows, err := service.Bindings(context.Background(), user.ID)
	if err != nil || len(rows) != 2 || rows[0].ID != "binding-old" && rows[0].ID != "binding-new" {
		t.Fatalf("bindings=%+v error=%v", rows, err)
	}
	for _, row := range rows {
		if row.Authority == "" || row.CreatedAt.IsZero() {
			t.Fatalf("incomplete binding=%+v", row)
		}
	}
}
