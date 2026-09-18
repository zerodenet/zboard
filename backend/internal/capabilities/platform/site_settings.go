package platform

import (
	"context"
	"net/url"
	"strings"
	"time"
)

type SiteSettingsInput struct {
	SiteName          string `json:"site_name"`
	SiteURL           string `json:"site_url"`
	AllowRegistration bool   `json:"allow_registration"`
}
type SiteSettingsView struct {
	SiteSettingsInput
	InstalledAt time.Time `json:"installed_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type SiteSettingsValidation struct{ Fields map[string]string }

func (e *SiteSettingsValidation) Error() string { return "站点设置校验失败。" }

func SiteSettingsValidationFields(name, address string) map[string]string {
	fields := map[string]string{}
	if name == "" || len(name) > 80 {
		fields["site_name"] = "站点名称必须为 1–80 个 UTF-8 字节。"
	}
	parsed, err := url.ParseRequestURI(address)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		fields["site_url"] = "请输入完整的 HTTP 或 HTTPS 地址。"
		return fields
	}
	if len(address) > 255 {
		fields["site_url"] = "公开访问地址不能超过 255 个 UTF-8 字节。"
	}
	if parsed.User != nil || parsed.Fragment != "" || strings.Contains(address, "#") {
		fields["site_url"] = "公开访问地址不能包含账号、密码或 URL 片段。"
	}
	return fields
}
func NormalizeSiteSettings(in SiteSettingsInput) (SiteSettingsInput, error) {
	in.SiteName = strings.TrimSpace(in.SiteName)
	in.SiteURL = strings.TrimRight(strings.TrimSpace(in.SiteURL), "/")
	if fields := SiteSettingsValidationFields(in.SiteName, in.SiteURL); len(fields) > 0 {
		return SiteSettingsInput{}, &SiteSettingsValidation{Fields: fields}
	}
	return in, nil
}

type SiteSettingsRepository interface {
	UpdateSite(context.Context, uint, SiteSettingsInput) (SiteSettingsView, error)
}
type SiteSettings struct{ Repository SiteSettingsRepository }

func (s SiteSettings) Update(ctx context.Context, actor uint, in SiteSettingsInput) (SiteSettingsView, error) {
	if actor == 0 {
		return SiteSettingsView{}, ErrSettingsPermission
	}
	in, err := NormalizeSiteSettings(in)
	if err != nil {
		return SiteSettingsView{}, err
	}
	return s.Repository.UpdateSite(ctx, actor, in)
}
