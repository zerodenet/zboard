package main

import (
	"testing"

	"golang.org/x/crypto/bcrypt"

	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
)

func TestBuildBootstrapAdminHashesPassword(t *testing.T) {
	config := cfgpkg.BootstrapAdmin{
		Username: "operator",
		Email:    "operator@example.com",
		Password: "strong-admin-password",
	}
	admin, err := buildBootstrapAdmin(config, cfgpkg.EnvironmentProduction)
	if err != nil {
		t.Fatalf("buildBootstrapAdmin() error = %v", err)
	}
	if admin.Username != config.Username || admin.Email != config.Email || !admin.IsAdmin || admin.Status != "active" {
		t.Fatalf("admin = %+v, want active configured administrator", admin)
	}
	if admin.Password == config.Password {
		t.Fatal("bootstrap password was stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(config.Password)); err != nil {
		t.Fatalf("stored bootstrap password hash does not match: %v", err)
	}
}

func TestBuildBootstrapAdminRejectsIncompleteConfig(t *testing.T) {
	_, err := buildBootstrapAdmin(cfgpkg.BootstrapAdmin{Username: "operator"}, cfgpkg.EnvironmentProduction)
	if err == nil {
		t.Fatal("buildBootstrapAdmin() error = nil, want incomplete config rejection")
	}
}
