package identitystore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginCommitRechecksAccountAndCurrentPrivileges(t *testing.T) {
	for _, change := range []string{"password", "status", "is_admin"} {
		t.Run(change, func(t *testing.T) {
			db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "accounts.db"))
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			defer pool.Close()
			if err := datastore.RunMigrations(db); err != nil {
				t.Fatal(err)
			}
			hash, err := bcrypt.GenerateFromPassword([]byte("KnownPassword2026!"), bcrypt.MinCost)
			if err != nil {
				t.Fatal(err)
			}
			user := model.User{Email: "owner@example.test", Password: string(hash), Status: "active", IsAdmin: true}
			if err := db.Create(&user).Error; err != nil {
				t.Fatal(err)
			}
			repo := Accounts{DB: db}
			ctx := context.Background()
			before, err := repo.LoginAccount(ctx, user.Email)
			if err != nil {
				t.Fatal(err)
			}
			value := map[string]any{"password": "replaced", "status": "suspended", "is_admin": false}[change]
			if err := db.Model(&user).Update(change, value).Error; err != nil {
				t.Fatal(err)
			}
			result, err := repo.CompleteLogin(ctx, before, time.Now().UTC())
			if change == "is_admin" {
				if err != nil || result.IsAdmin {
					t.Fatal("stale privilege returned", result, err)
				}
			} else {
				if !errors.Is(err, identity.ErrUnavailable) {
					t.Fatal("stale credential accepted", err)
				}
				var after model.User
				if err := db.First(&after, user.ID).Error; err != nil {
					t.Fatal(err)
				}
				if after.LastLoginAt != nil {
					t.Fatal("failed login updated account")
				}
			}
		})
	}
}

func TestAccountServiceLoginAndSelfScope(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("KnownPassword2026!"), bcrypt.MinCost)
	user := model.User{Email: "owner@example.test", Password: string(hash), Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.Accounts{Repository: Accounts{DB: db}}
	ctx := context.Background()
	result, err := service.Login(ctx, " OWNER@EXAMPLE.TEST ", "KnownPassword2026!")
	if err != nil || result.ID != user.ID {
		t.Fatal(result, err)
	}
	for _, input := range []struct{ email, password string }{{user.Email, "wrong"}, {"missing@example.test", "KnownPassword2026!"}} {
		if _, err := service.Login(ctx, input.email, input.password); !errors.Is(err, identity.ErrCredentials) {
			t.Fatal(err)
		}
	}
	if _, err := service.Me(ctx, identity.Principal{}); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := service.Me(ctx, identity.Principal{ID: user.ID + 1}); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatal(err)
	}
}
