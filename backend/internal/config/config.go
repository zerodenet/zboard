package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/rest"
)

const (
	EnvironmentDevelopment = "development"
	EnvironmentProduction  = "production"
	EnvironmentTest        = "test"
	MinimumJWTSecretBytes  = 32
)

type Config struct {
	rest.RestConf
	Environment            string `json:"environment,default=development"`
	DataSource             string `json:"datasource"`
	RedisAddr              string `json:"redis_addr,optional"`
	JwtSecret              string `json:"jwt_secret"`
	BootstrapAdminUsername string `json:"bootstrap_admin_username,optional"`
	BootstrapAdminEmail    string `json:"bootstrap_admin_email,optional"`
	BootstrapAdminPassword string `json:"bootstrap_admin_password,optional"`
}

type BootstrapAdmin struct {
	Username string
	Email    string
	Password string
}

func (c *Config) ApplyEnvironment(getenv func(string) string) {
	if getenv == nil {
		return
	}
	applyOverride(&c.Environment, getenv("ZBOARD_ENVIRONMENT"))
	applyOverride(&c.DataSource, getenv("ZBOARD_DATA_SOURCE"))
	if value := getenv("ZBOARD_REDIS_ADDR"); strings.TrimSpace(value) != "" {
		c.RedisAddr = value
	} else {
		applyOverride(&c.RedisAddr, getenv("ZBOARD_REDIS"))
	}
	applyOverride(&c.JwtSecret, getenv("ZBOARD_JWT_SECRET"))
	applyOverride(&c.BootstrapAdminUsername, getenv("ZBOARD_BOOTSTRAP_ADMIN_USERNAME"))
	applyOverride(&c.BootstrapAdminEmail, getenv("ZBOARD_BOOTSTRAP_ADMIN_EMAIL"))
	applyOverride(&c.BootstrapAdminPassword, getenv("ZBOARD_BOOTSTRAP_ADMIN_PASSWORD"))
}

func (c *Config) Validate() error {
	environment, err := NormalizeEnvironment(c.Environment)
	if err != nil {
		return err
	}
	c.Environment = environment
	if strings.TrimSpace(c.DataSource) == "" {
		return errors.New("datasource is required")
	}
	if err := ValidateJWTSecret(c.JwtSecret); err != nil {
		return err
	}

	admin := c.BootstrapAdmin()
	if admin.Configured() {
		return admin.Validate(environment)
	}
	return nil
}

func (c Config) BootstrapAdmin() BootstrapAdmin {
	return BootstrapAdmin{
		Username: strings.TrimSpace(c.BootstrapAdminUsername),
		Email:    strings.TrimSpace(c.BootstrapAdminEmail),
		Password: c.BootstrapAdminPassword,
	}
}

func NormalizeEnvironment(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return EnvironmentDevelopment, nil
	}
	switch normalized {
	case "dev":
		return EnvironmentDevelopment, nil
	case "prod":
		return EnvironmentProduction, nil
	case EnvironmentDevelopment, EnvironmentProduction, EnvironmentTest:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported environment %q", value)
	}
}

func ValidateJWTSecret(secret string) error {
	trimmed := strings.TrimSpace(secret)
	if isPlaceholder(trimmed) {
		return errors.New("jwt_secret uses a known insecure placeholder")
	}
	if len(trimmed) < MinimumJWTSecretBytes {
		return fmt.Errorf("jwt_secret must contain at least %d bytes", MinimumJWTSecretBytes)
	}
	return nil
}

func (a BootstrapAdmin) Configured() bool {
	return a.Username != "" || a.Email != "" || a.Password != ""
}

func (a BootstrapAdmin) Validate(environment string) error {
	if a.Username == "" || a.Email == "" || a.Password == "" {
		return errors.New("bootstrap admin requires username, email, and password")
	}
	if !strings.Contains(a.Email, "@") {
		return errors.New("bootstrap admin email is invalid")
	}
	if isPlaceholder(a.Username) || isPlaceholder(a.Email) || isPlaceholder(a.Password) {
		return errors.New("bootstrap admin contains an insecure placeholder")
	}
	minimumPasswordBytes := 12
	if environment == EnvironmentProduction {
		minimumPasswordBytes = 16
	}
	if len(a.Password) < minimumPasswordBytes {
		return fmt.Errorf("bootstrap admin password must contain at least %d bytes", minimumPasswordBytes)
	}
	return nil
}

func applyOverride(target *string, value string) {
	if strings.TrimSpace(value) != "" {
		*target = value
	}
}

func isPlaceholder(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "change-me" ||
		normalized == "changeme" ||
		normalized == "dev-jwt-secret" ||
		strings.HasPrefix(normalized, "replace-") ||
		strings.HasPrefix(normalized, "generate-") ||
		strings.HasPrefix(normalized, "choose-")
}
