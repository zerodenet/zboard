package platform

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

type SMTPSettings struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string
}

func ValidateSMTPDeliverySettings(settings SMTPSettings, requireEnabled bool) error {
	if requireEnabled && !settings.Enabled {
		return errors.New("email tasks are disabled")
	}
	if settings.Host == "" || settings.Port < 1 || settings.Port > 65535 || !identity.ValidEmail(settings.From) {
		return errors.New("SMTP host, port and from address must be configured")
	}
	if settings.TLSMode != "starttls" && settings.TLSMode != "implicit" {
		return errors.New("smtp_tls_mode must be starttls or implicit")
	}
	if settings.Username != "" && settings.Password == "" {
		return errors.New("smtp_password is required when smtp_username is configured")
	}
	return nil
}
