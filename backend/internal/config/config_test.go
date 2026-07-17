package config

import (
	"strings"
	"testing"
)

const testJWTSecret = "0123456789abcdef0123456789abcdef"
const testCredentialEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestApplyEnvironmentOverridesSecuritySettings(t *testing.T) {
	values := map[string]string{
		"ZBOARD_ENVIRONMENT":               "production",
		"ZBOARD_DATA_SOURCE":               "zboard:strong-db-password@tcp(mysql:3306)/zboard",
		"ZBOARD_REDIS_ADDR":                "redis:6379",
		"ZBOARD_JWT_SECRET":                testJWTSecret,
		"ZBOARD_BOOTSTRAP_ADMIN_USERNAME":  "operator",
		"ZBOARD_BOOTSTRAP_ADMIN_EMAIL":     "operator@example.com",
		"ZBOARD_BOOTSTRAP_ADMIN_PASSWORD":  "strong-admin-password",
		"ZBOARD_CREDENTIAL_ENCRYPTION_KEY": testCredentialEncryptionKey,
	}
	c := Config{}
	c.ApplyEnvironment(func(key string) string { return values[key] })

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if c.Environment != EnvironmentProduction || c.RedisAddr != "redis:6379" {
		t.Fatalf("environment overrides not applied: %+v", c)
	}
	if admin := c.BootstrapAdmin(); admin.Username != "operator" || admin.Email != "operator@example.com" {
		t.Fatalf("bootstrap admin overrides not applied: %+v", admin)
	}
}

func TestValidateRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{name: "unsupported environment", edit: func(c *Config) { c.Environment = "stage" }, want: "unsupported environment"},
		{name: "missing datasource", edit: func(c *Config) { c.DataSource = "" }, want: "datasource is required"},
		{name: "weak jwt", edit: func(c *Config) { c.JwtSecret = "dev-jwt-secret" }, want: "jwt_secret"},
		{name: "jwt placeholder", edit: func(c *Config) { c.JwtSecret = "generate-at-least-32-random-bytes" }, want: "placeholder"},
		{name: "partial bootstrap", edit: func(c *Config) { c.BootstrapAdminPassword = "" }, want: "requires username"},
		{name: "bootstrap placeholder", edit: func(c *Config) { c.BootstrapAdminPassword = "generate-at-least-16-random-bytes" }, want: "placeholder"},
		{name: "short production bootstrap password", edit: func(c *Config) { c.BootstrapAdminPassword = "123456789012" }, want: "at least 16"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				Environment:             EnvironmentProduction,
				DataSource:              "zboard:strong-db-password@tcp(mysql:3306)/zboard",
				JwtSecret:               testJWTSecret,
				BootstrapAdminUsername:  "operator",
				BootstrapAdminEmail:     "operator@example.com",
				BootstrapAdminPassword:  "strong-admin-password",
				CredentialEncryptionKey: testCredentialEncryptionKey,
			}
			tt.edit(&c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestBootstrapAdminMayBeOmittedForExistingDatabase(t *testing.T) {
	c := Config{
		Environment:             EnvironmentProduction,
		DataSource:              "zboard:strong-db-password@tcp(mysql:3306)/zboard",
		JwtSecret:               testJWTSecret,
		CredentialEncryptionKey: testCredentialEncryptionKey,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if c.BootstrapAdmin().Configured() {
		t.Fatal("BootstrapAdmin().Configured() = true, want false")
	}
}
