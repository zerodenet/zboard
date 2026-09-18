package platform

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"golang.org/x/crypto/bcrypt"
)

var ErrAlreadyInstalled = errors.New("zboard is already installed")

type InstallationValidation struct {
	Message string
	Fields  map[string]string
}

func (e *InstallationValidation) Error() string { return e.Message }

type InstallationResult struct {
	SiteName    string
	Account     identity.PublicAccount
	Preferences SetupPreferences
}
type InstallationStatus struct {
	Installed bool `json:"installed"`
	SiteSettingsInput
	InstalledAt time.Time `json:"installed_at,omitempty"`
}
type InstallationRepository interface {
	Status(context.Context) (InstallationStatus, error)
	Create(context.Context, InstallationInput, SetupPreferences, string) (InstallationResult, error)
}
type Installation struct{ Repository InstallationRepository }

func NormalizeInstallation(in InstallationInput) (InstallationInput, error) {
	in.SiteName = strings.TrimSpace(in.SiteName)
	in.SiteURL = strings.TrimRight(strings.TrimSpace(in.SiteURL), "/")
	in.AdminEmail = identity.NormalizeEmail(in.AdminEmail)
	fields := SiteSettingsValidationFields(in.SiteName, in.SiteURL)
	if !identity.ValidEmail(in.AdminEmail) {
		fields["admin_email"] = "请输入有效的管理员邮箱。"
	}
	if len(in.AdminPassword) < 12 || len(in.AdminPassword) > 72 {
		fields["admin_password"] = "管理员密码必须为 12–72 个 UTF-8 字节。"
	}
	if len(fields) > 0 {
		return InstallationInput{}, &InstallationValidation{Message: "安装信息校验失败。", Fields: fields}
	}
	return in, nil
}
func (s Installation) Create(ctx context.Context, in InstallationInput) (InstallationResult, error) {
	in, err := NormalizeInstallation(in)
	if err != nil {
		return InstallationResult{}, err
	}
	prefs, err := NormalizeSetupPreferences(in)
	if err != nil {
		return InstallationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstallationResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return InstallationResult{}, err
	}
	in.AdminPassword = ""
	return s.Repository.Create(ctx, in, prefs, string(hash))
}
func (s Installation) Status(ctx context.Context) (InstallationStatus, error) {
	return s.Repository.Status(ctx)
}
