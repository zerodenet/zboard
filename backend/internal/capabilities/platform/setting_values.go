package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"net/url"
	"strconv"
	"strings"
)

func NormalizeSettingValue(config Setting, raw json.RawMessage) (string, error) {
	switch config.ValueType {
	case "string":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("value must be a string")
		}
		if len(value) > 100000 {
			return "", errors.New("string config value is too long")
		}
		return strings.TrimSpace(value), nil
	case "bool":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("value must be a boolean")
		}
		return strconv.FormatBool(value), nil
	case "int":
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("value must be an integer")
		}
		return strconv.FormatInt(value, 10), nil
	case "json":
		var value interface{}
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("value must be valid JSON")
		}
		canonical, err := json.Marshal(value)
		return string(canonical), err
	default:
		return "", fmt.Errorf("unsupported value type %q", config.ValueType)
	}
}

func ValidateSettingValue(key, value string) error {
	switch key {
	case "site_name":
		if value == "" || len(value) > 80 {
			return errors.New("site_name must contain 1 to 80 bytes")
		}
	case "site_url", "subscribe_url", "site_logo", "subscription_camouflage_url":
		if value == "" && key != "site_url" {
			return nil
		}
		if key == "site_url" && len(value) > 255 {
			return errors.New("site_url must not exceed 255 bytes")
		}
		if len(value) > 2048 {
			return fmt.Errorf("%s must not exceed 2048 bytes", key)
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" || strings.Contains(value, "#") {
			return fmt.Errorf("%s must be an absolute http or https URL", key)
		}
	case "site_desc":
		if len(value) > 500 {
			return errors.New("site_desc must not exceed 500 bytes")
		}
	case "smtp_host":
		if len(value) > 255 || strings.ContainsAny(value, "/: \t\r\n") ||
			strings.Contains(value, "..") || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") ||
			strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
			return errors.New("smtp_host must be a hostname without a scheme or port")
		}
		for _, ch := range value {
			if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '.' && ch != '-' {
				return errors.New("smtp_host contains invalid characters")
			}
		}
	case "smtp_port":
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return errors.New("smtp_port must be between 1 and 65535")
		}
	case "smtp_from":
		if value != "" && !identity.ValidEmail(value) {
			return errors.New("smtp_from must be a valid email address")
		}
	case "smtp_tls_mode":
		if value != "starttls" && value != "implicit" {
			return errors.New("smtp_tls_mode must be starttls or implicit")
		}
	case "smtp_username":
		if len(value) > 255 || strings.ContainsAny(value, "\r\n") {
			return errors.New("smtp_username is invalid")
		}
	case "smtp_password":
		if len(value) > 4096 {
			return errors.New("smtp_password is too long")
		}
	case "maintenance_title":
		if strings.TrimSpace(value) == "" || len(value) > 160 {
			return errors.New("maintenance_title must contain 1 to 160 bytes")
		}
	case "maintenance_message":
		if strings.TrimSpace(value) == "" || len(value) > 4000 {
			return errors.New("maintenance_message must contain 1 to 4000 bytes")
		}
	case "maintenance_task_id":
		id, err := strconv.ParseUint(value, 10, 64)
		if err != nil || id > uint64(^uint(0)) {
			return errors.New("maintenance_task_id must be a non-negative integer")
		}
	}
	return nil
}
