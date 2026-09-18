package identitystore

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAdministrationKeepsLastAdminAndAuditsAtomically(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	actor := model.User{Email: "admin@example.test", Status: "active", IsAdmin: true, Password: "hash"}
	if err := db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	service := identity.Administration{Repository: Administration{DB: db}}
	ctx := context.Background()
	p := identity.Principal{ID: actor.ID, Actor: "untrusted-label"}
	no := false
	if _, err := service.Update(ctx, p, actor.ID, identity.AccountChange{IsAdmin: &no}); !errors.Is(err, identity.ErrLastAdministrator) {
		t.Fatal(err)
	}
	target, err := service.Create(ctx, p, identity.NewAccount{Email: " new@example.test ", Password: "ValidPassword2026!"})
	if err != nil {
		t.Fatal(err)
	}
	if target.Email != "new@example.test" || target.Status != "active" {
		t.Fatal(target)
	}
	if _, err := service.Create(ctx, identity.Principal{ID: target.ID}, identity.NewAccount{Email: "attacker@example.test", Password: "ValidPassword2026!", IsAdmin: true}); !errors.Is(err, identity.ErrPermission) {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, p, identity.NewAccount{Email: target.Email, Password: "ValidPassword2026!"}); !errors.Is(err, identity.ErrEmailConflict) {
		t.Fatal(err)
	}
	var audit model.AuditLog
	if err := db.Where("action = ?", "user.create").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Actor != actor.Email {
		t.Fatal("audit used caller label", audit.Actor)
	}
	if err := db.Exec("CREATE TRIGGER reject_account_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'user.update' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	suspended := "suspended"
	if _, err := service.Update(ctx, p, target.ID, identity.AccountChange{Status: &suspended}); err == nil {
		t.Fatal("expected audit failure")
	}
	var current model.User
	if err := db.First(&current, target.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != "active" {
		t.Fatal("mutation committed without audit", current.Status)
	}
}

func TestConcurrentDemotionsCannotRemoveAllAdministrators(t *testing.T) {
	a, b := administrationFixture(t)
	users := []model.User{{Email: "a@example.test", Status: "active", IsAdmin: true, Password: "hash"}, {Email: "b@example.test", Status: "active", IsAdmin: true, Password: "hash"}}
	if err := a.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	services := []identity.Administration{{Repository: Administration{DB: a}}, {Repository: Administration{DB: b}}}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i, s := range services {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			no := false
			_, err := s.Update(context.Background(), identity.Principal{ID: users[i].ID}, users[i].ID, identity.AccountChange{IsAdmin: &no})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("expected one committed demotion", successes)
	}
	var count int64
	if err := a.Model(&model.User{}).Where("is_admin = ? AND status = ?", true, "active").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("administrator invariant lost", count)
	}
}
