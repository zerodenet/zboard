package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type setupInstallWithPreferencesRequest struct {
	SiteName                 string  `json:"site_name"`
	SiteURL                  string  `json:"site_url"`
	AllowRegistration        bool    `json:"allow_registration"`
	AdminEmail               string  `json:"admin_email"`
	AdminPassword            string  `json:"admin_password"`
	SystemTimezone           *string `json:"system_timezone,omitempty"`
	AuditLogRetentionDays    *int    `json:"audit_log_retention_days,omitempty"`
	OperationRetentionDays   *int    `json:"operation_history_retention_days,omitempty"`
	TaskHistoryRetentionDays *int    `json:"task_history_retention_days,omitempty"`
}

type setupSystemPreferences struct {
	SystemTimezone           string `json:"system_timezone"`
	AuditLogRetentionDays    int    `json:"audit_log_retention_days"`
	OperationRetentionDays   int    `json:"operation_history_retention_days"`
	TaskHistoryRetentionDays int    `json:"task_history_retention_days"`
}

func defaultSetupSystemPreferences() setupSystemPreferences {
	return setupSystemPreferences{
		SystemTimezone:           defaultSystemTimezone,
		AuditLogRetentionDays:    defaultAuditRetentionDays,
		OperationRetentionDays:   defaultOperationRetention,
		TaskHistoryRetentionDays: defaultTaskRetentionDays,
	}
}

func normalizeSetupSystemPreferences(body setupInstallWithPreferencesRequest) (setupSystemPreferences, error) {
	preferences := defaultSetupSystemPreferences()
	fields := map[string]string{}

	if body.SystemTimezone != nil {
		preferences.SystemTimezone = strings.TrimSpace(*body.SystemTimezone)
	}
	if preferences.SystemTimezone == "" {
		fields[systemTimezoneKey] = "请输入有效的 IANA 时区，例如 Asia/Shanghai。"
	} else if _, err := time.LoadLocation(preferences.SystemTimezone); err != nil {
		fields[systemTimezoneKey] = "请输入有效的 IANA 时区，例如 Asia/Shanghai。"
	}

	for _, item := range []struct {
		key      string
		provided *int
		target   *int
	}{
		{auditLogRetentionKey, body.AuditLogRetentionDays, &preferences.AuditLogRetentionDays},
		{operationRetentionKey, body.OperationRetentionDays, &preferences.OperationRetentionDays},
		{taskRetentionKey, body.TaskHistoryRetentionDays, &preferences.TaskHistoryRetentionDays},
	} {
		if item.provided != nil {
			*item.target = *item.provided
		}
		if *item.target < 0 || *item.target > historyRetentionMaxDays {
			fields[item.key] = "保留天数必须是 0–3650 之间的整数；0 表示永久保留。"
		}
	}

	if len(fields) > 0 {
		return setupSystemPreferences{}, validationError("系统策略校验失败。", fields)
	}
	return preferences, nil
}

func upsertSetupSystemPreferences(tx *gorm.DB, preferences setupSystemPreferences) error {
	values := map[string]string{
		systemTimezoneKey:     preferences.SystemTimezone,
		auditLogRetentionKey:  strconv.Itoa(preferences.AuditLogRetentionDays),
		operationRetentionKey: strconv.Itoa(preferences.OperationRetentionDays),
		taskRetentionKey:      strconv.Itoa(preferences.TaskHistoryRetentionDays),
	}
	definitions := make(map[string]model.SystemConfig, len(values))
	for _, definition := range historyRetentionDefaults() {
		definitions[definition.ConfigKey] = definition
	}
	for key, value := range values {
		var current model.SystemConfig
		err := tx.Where("config_key = ?", key).First(&current).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			definition, ok := definitions[key]
			if !ok {
				return errors.New("setup system preference definition not found: " + key)
			}
			definition.Value = value
			if err := tx.Create(&definition).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := tx.Model(&current).Updates(map[string]interface{}{
				"value":    value,
				"revision": gorm.Expr("revision + 1"),
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// SetupInstallWithSystemPreferencesHandler keeps first installation atomic while
// allowing the browser wizard to initialize system-wide calendar and retention
// policy. All preference fields are optional so older setup clients keep the
// server defaults instead of becoming incompatible with the expanded wizard.
func (h *handlers) SetupInstallWithSystemPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	var body setupInstallWithPreferencesRequest
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	legacy := setupRequest{
		SiteName:          body.SiteName,
		SiteURL:           body.SiteURL,
		AllowRegistration: body.AllowRegistration,
		AdminEmail:        body.AdminEmail,
		AdminPassword:     body.AdminPassword,
	}
	if err := validateSetupRequest(&legacy); err != nil {
		BadRequestError(w, err)
		return
	}
	preferences, err := normalizeSetupSystemPreferences(body)
	if err != nil {
		BadRequestError(w, err)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(legacy.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		ServerError(w, err)
		return
	}
	admin := model.User{
		AccountName: legacy.AdminEmail,
		Email:       legacy.AdminEmail,
		Password:    string(hash),
		IsAdmin:     true,
		Status:      userStatusActive,
	}
	installation := model.Installation{
		ID:                1,
		SiteName:          legacy.SiteName,
		SiteURL:           legacy.SiteURL,
		AllowRegistration: legacy.AllowRegistration,
		InstalledAt:       time.Now().UTC(),
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var userCount int64
		if err := tx.Model(&model.User{}).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount > 0 {
			return errAlreadyInstalled
		}
		if err := tx.Create(&installation).Error; err != nil {
			if isDuplicateError(err) {
				return errAlreadyInstalled
			}
			return err
		}
		if err := upsertSiteConfigs(tx, installation.SiteName, installation.SiteURL, installation.AllowRegistration); err != nil {
			return err
		}
		if err := upsertSetupSystemPreferences(tx, preferences); err != nil {
			return err
		}
		return tx.Create(&admin).Error
	})
	if errors.Is(err, errAlreadyInstalled) || isDuplicateError(err) {
		writeJSON(w, http.StatusConflict, "zboard is already installed", nil)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}

	token, expiresAt, err := h.issueToken(authClaims{UserID: admin.ID, Email: admin.Email, IsAdmin: true})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{
		"installed":          true,
		"site_name":          installation.SiteName,
		"system_preferences": preferences,
		"user":               toPublicUser(admin),
		"auth":               tokenResponse{Token: token, ExpiresAt: expiresAt},
	})
}
