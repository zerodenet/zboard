package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	"gorm.io/gorm"
)

const (
	maxSitePolicyContentBytes = 48 * 1024
	maxSiteURLBytes           = 2048
)

var sitePolicyContentKeys = map[string]struct{}{
	"site_terms_content":   {},
	"site_privacy_content": {},
	"site_refund_content":  {},
}

func (config *SystemConfig) BeforeSave(_ *gorm.DB) error {
	if config == nil {
		return nil
	}
	return validateSiteSystemConfig(config.ConfigKey, config.Value)
}

func validateSiteSystemConfig(key, value string) error {
	if _, ok := sitePolicyContentKeys[key]; ok {
		if len(value) > maxSitePolicyContentBytes {
			return fmt.Errorf("%s must not exceed %d UTF-8 bytes", key, maxSitePolicyContentBytes)
		}
		return nil
	}

	switch key {
	case "site_logo_dark", "site_favicon":
		return validateOptionalPublicURL(key, value)
	case "site_support_url", "site_telegram_url":
		return validateOptionalPublicURL(key, value)
	case "site_support_email":
		if value == "" {
			return nil
		}
		if len(value) > 254 || strings.ContainsAny(value, "\r\n") {
			return errors.New("site_support_email must be a valid email address")
		}
		parsed, err := mail.ParseAddress(value)
		if err != nil || parsed.Address != value {
			return errors.New("site_support_email must be a valid email address")
		}
	case "site_footer_copyright":
		if len(value) > 1024 {
			return errors.New("site_footer_copyright must not exceed 1024 UTF-8 bytes")
		}
	case "site_meta_title":
		if len(value) > 180 {
			return errors.New("site_meta_title must not exceed 180 UTF-8 bytes")
		}
	case "site_meta_description":
		if len(value) > 1024 {
			return errors.New("site_meta_description must not exceed 1024 UTF-8 bytes")
		}
	case "site_home_kicker":
		if len(value) > 120 {
			return errors.New("site_home_kicker must not exceed 120 UTF-8 bytes")
		}
	case "site_home_title":
		if len(value) > 1024 {
			return errors.New("site_home_title must not exceed 1024 UTF-8 bytes")
		}
	case "site_legal_items":
		return validateSiteLegalItems(value)
	}
	return nil
}

func validateOptionalPublicURL(key, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxSiteURLBytes {
		return fmt.Errorf("%s must not exceed %d UTF-8 bytes", key, maxSiteURLBytes)
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" || strings.Contains(value, "#") {
		return fmt.Errorf("%s must be an absolute http or https URL", key)
	}
	return nil
}

type siteLegalItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
	URL   string `json:"url,omitempty"`
}

func validateSiteLegalItems(value string) error {
	if len(value) > 16*1024 {
		return errors.New("site_legal_items must not exceed 16384 UTF-8 bytes")
	}
	var items []siteLegalItem
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return errors.New("site_legal_items must be a JSON array")
	}
	if len(items) > 32 {
		return errors.New("site_legal_items must not contain more than 32 items")
	}
	for index, item := range items {
		if strings.TrimSpace(item.Label) == "" || strings.TrimSpace(item.Value) == "" {
			return fmt.Errorf("site_legal_items item %d requires label and value", index+1)
		}
		if len(item.Label) > 120 || len(item.Value) > 512 {
			return fmt.Errorf("site_legal_items item %d is too long", index+1)
		}
		if item.URL != "" {
			if err := validateOptionalPublicURL(fmt.Sprintf("site_legal_items item %d url", index+1), item.URL); err != nil {
				return err
			}
		}
	}
	return nil
}
