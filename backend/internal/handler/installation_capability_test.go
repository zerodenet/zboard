package handler

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestPlatformInstallationHasOneWinnerAndAtomicAccount(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Unscoped().Where("email = ?", "reader@example.test").Delete(&model.User{}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := platform.InstallationInput{SiteName: "First", SiteURL: "https://first.example", AdminEmail: "first@example.test", AdminPassword: "a-long-test-password", AllowRegistration: true}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := h.services.Installation.Create(ctx, in); results <- err }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, platform.ErrAlreadyInstalled) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatal("installation winner count", winners)
	}
	state, err := h.services.Installation.Status(ctx)
	if err != nil || !state.Installed || state.SiteName != "First" {
		t.Fatal(state, err)
	}
	var users []model.User
	if err := h.db.Find(&users).Error; err != nil || len(users) != 1 {
		t.Fatal(len(users), err)
	}
	if !users[0].IsAdmin || bcrypt.CompareHashAndPassword([]byte(users[0].Password), []byte(in.AdminPassword)) != nil {
		t.Fatal("initial administrator invalid")
	}
	var timezone model.SystemConfig
	if err := h.db.Where("config_key = ?", "system_timezone").First(&timezone).Error; err != nil || timezone.Value != "UTC" {
		t.Fatal(timezone, err)
	}
}

func TestPlatformInstallationRollsBackAndRejectsDeletedAccounts(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Unscoped().Where("email = ?", "reader@example.test").Delete(&model.User{}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := platform.InstallationInput{SiteName: "First", SiteURL: "https://first.example", AdminEmail: "first@example.test", AdminPassword: "a-long-test-password"}
	const name = "installation-account-failure"
	accountFailure := errors.New("fixture account creation failed")
	if err := h.db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(accountFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := h.services.Installation.Create(ctx, in)
	h.db.Callback().Create().Remove(name)
	if !errors.Is(err, accountFailure) {
		t.Fatal("account failure not reached", err)
	}
	state, err := h.services.Installation.Status(ctx)
	if err != nil || state.Installed {
		t.Fatal("installation marker escaped rollback", state, err)
	}
	var preferences int64
	if err := h.db.Model(&model.SystemConfig{}).Where("config_key = ?", "system_timezone").Count(&preferences).Error; err != nil || preferences != 0 {
		t.Fatal("preferences escaped rollback", preferences, err)
	}
	user := model.User{Email: "deleted@example.test", Password: "unused", Status: "active"}
	if err := h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := h.services.Installation.Create(ctx, in); !errors.Is(err, platform.ErrAlreadyInstalled) {
		t.Fatal("deleted account permitted reinitialization", err)
	}
}
