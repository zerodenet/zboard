package config

import (
	"strings"
	"testing"
)

const testJWTSecret = "0123456789abcdef0123456789abcdef"
const testCredentialEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestApplyEnvironmentOverridesSecuritySettings(t *testing.T) {
	values := map[string]string{
		"ZBOARD_ENVIRONMENT":                              "production",
		"ZBOARD_DATA_SOURCE":                              "zboard:strong-db-password@tcp(mysql:3306)/zboard",
		"ZBOARD_DATABASE_MAX_OPEN_CONNECTIONS":            "8",
		"ZBOARD_DATABASE_MAX_IDLE_CONNECTIONS":            "2",
		"ZBOARD_DATABASE_CONNECTION_MAX_LIFETIME_SECONDS": "600",
		"ZBOARD_JWT_SECRET":                               testJWTSecret,
		"ZBOARD_BOOTSTRAP_ADMIN_EMAIL":                    "operator@example.com",
		"ZBOARD_BOOTSTRAP_ADMIN_PASSWORD":                 "strong-admin-password",
		"ZBOARD_CREDENTIAL_ENCRYPTION_KEY":                testCredentialEncryptionKey,
		"ZBOARD_ZERO_ARTIFACT_DIR":                        "/var/lib/zboard/artifacts",
		"ZBOARD_ZERO_KERNEL_CONTRACT":                     "native-local",
		"ZBOARD_ZERO_LOCAL_VERSION":                       "0.0.15-rc.1",
	}
	c := Config{}
	c.ApplyEnvironment(func(key string) string { return values[key] })

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if c.Environment != EnvironmentProduction || c.DatabaseMaxOpenConnections != 8 ||
		c.DatabaseMaxIdleConnections != 2 || c.DatabaseConnectionMaxLifetimeSecs != 600 ||
		c.ZeroArtifactDir != "/var/lib/zboard/artifacts" || c.ZeroKernelContract != ZeroKernelNativeLocal ||
		c.ZeroLocalVersion != "0.0.15-rc.1" {
		t.Fatalf("environment overrides not applied: %+v", c)
	}
	if admin := c.BootstrapAdmin(); admin.Email != "operator@example.com" {
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
		{name: "unsupported Zero contract", edit: func(c *Config) { c.ZeroKernelContract = "github-next" }, want: "zero_kernel_contract"},
		{name: "missing local Zero version", edit: func(c *Config) { c.ZeroKernelContract = ZeroKernelNativeLocal }, want: "zero_local_version"},
		{name: "missing Mieru local Zero version", edit: func(c *Config) { c.ZeroKernelContract = ZeroKernelNativeMieru }, want: "zero_local_version"},
		{name: "invalid local Zero version", edit: func(c *Config) { c.ZeroLocalVersion = "../zero" }, want: "zero_local_version"},
		{name: "missing datasource", edit: func(c *Config) { c.DataSource = "" }, want: "datasource is required"},
		{name: "too many database connections", edit: func(c *Config) { c.DatabaseMaxOpenConnections = 257 }, want: "database_max_open_connections"},
		{name: "too many idle database connections", edit: func(c *Config) { c.DatabaseMaxOpenConnections = 4; c.DatabaseMaxIdleConnections = 5 }, want: "database_max_idle_connections"},
		{name: "short database connection lifetime", edit: func(c *Config) { c.DatabaseConnectionMaxLifetimeSecs = 59 }, want: "database_connection_max_lifetime_seconds"},
		{name: "weak jwt", edit: func(c *Config) { c.JwtSecret = "dev-jwt-secret" }, want: "jwt_secret"},
		{name: "jwt placeholder", edit: func(c *Config) { c.JwtSecret = "generate-at-least-32-random-bytes" }, want: "placeholder"},
		{name: "partial bootstrap", edit: func(c *Config) { c.BootstrapAdminPassword = "" }, want: "requires email"},
		{name: "bootstrap placeholder", edit: func(c *Config) { c.BootstrapAdminPassword = "generate-at-least-16-random-bytes" }, want: "placeholder"},
		{name: "short production bootstrap password", edit: func(c *Config) { c.BootstrapAdminPassword = "123456789012" }, want: "at least 16"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				Environment:             EnvironmentProduction,
				DataSource:              "zboard:strong-db-password@tcp(mysql:3306)/zboard",
				JwtSecret:               testJWTSecret,
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

func TestValidateAppliesLowResourceDatabasePoolDefaults(t *testing.T) {
	c := Config{
		Environment:             EnvironmentProduction,
		DataSource:              "zboard:strong-db-password@tcp(mysql:3306)/zboard",
		JwtSecret:               testJWTSecret,
		CredentialEncryptionKey: testCredentialEncryptionKey,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if c.DatabaseMaxOpenConnections != 8 || c.DatabaseMaxIdleConnections != 2 || c.DatabaseConnectionMaxLifetimeSecs != 3600 {
		t.Fatalf("database pool defaults = %d/%d/%d", c.DatabaseMaxOpenConnections, c.DatabaseMaxIdleConnections, c.DatabaseConnectionMaxLifetimeSecs)
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
