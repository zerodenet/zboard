package handler

import (
	"errors"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func siteCustomizationDefaults() []model.SystemConfig {
	return []model.SystemConfig{
		{
			ConfigKey:   "site_logo_dark",
			Name:        "深色站点 Logo",
			Value:       "",
			ValueType:   "string",
			Description: "深色背景使用的 Logo URL；为空时回退到站点 Logo。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_favicon",
			Name:        "站点图标",
			Value:       "",
			ValueType:   "string",
			Description: "浏览器标签页和收藏夹使用的 favicon URL。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_footer_copyright",
			Name:        "页脚版权",
			Value:       "",
			ValueType:   "string",
			Description: "公开站点页脚显示的版权文本；为空时自动使用站点名称和当前年份。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_support_email",
			Name:        "客服邮箱",
			Value:       "",
			ValueType:   "string",
			Description: "公开站点用于联系支持团队的邮箱地址。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_support_url",
			Name:        "客服入口",
			Value:       "",
			ValueType:   "string",
			Description: "公开站点的客服、工单或帮助中心 URL。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_telegram_url",
			Name:        "Telegram 社区",
			Value:       "",
			ValueType:   "string",
			Description: "公开站点展示的 Telegram 群组或频道 URL。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_terms_url",
			Name:        "服务条款",
			Value:       "",
			ValueType:   "string",
			Description: "服务条款页面 URL；为空时不展示。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_privacy_url",
			Name:        "隐私政策",
			Value:       "",
			ValueType:   "string",
			Description: "隐私政策页面 URL；为空时不展示。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_refund_url",
			Name:        "退款政策",
			Value:       "",
			ValueType:   "string",
			Description: "退款或取消政策页面 URL；为空时不展示。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_legal_items",
			Name:        "法律与注册信息",
			Value:       "[]",
			ValueType:   "json",
			Description: "可选的地区中立法律或注册信息数组。每项支持 label、value 和可选 url，例如 Company No.、VAT 或当地备案信息。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_meta_title",
			Name:        "SEO 标题",
			Value:       "",
			ValueType:   "string",
			Description: "浏览器标题和基础 SEO 标题；为空时使用站点名称。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_meta_description",
			Name:        "SEO 描述",
			Value:       "",
			ValueType:   "string",
			Description: "页面 meta description；为空时使用站点描述。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_home_kicker",
			Name:        "首页提示语",
			Value:       "灵活套餐 · 独立订阅 · 清晰计费",
			ValueType:   "string",
			Description: "首页主标题上方的短提示语。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_home_title",
			Name:        "首页主标题",
			Value:       "选择适合你的套餐，按需订阅，轻松管理。",
			ValueType:   "string",
			Description: "首页 Hero 区域的主标题。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_home_primary_cta",
			Name:        "首页主按钮",
			Value:       "浏览套餐",
			ValueType:   "string",
			Description: "首页 Hero 区域主按钮文本，目标固定为套餐页。",
			IsPublic:    true,
			Revision:    1,
		},
	}
}

// ReconcileSiteCustomizationDefaults follows the existing runtime-default
// pattern used by other SystemConfig-backed policies. Missing keys are added
// for both fresh and existing prerelease databases, while operator values are
// never overwritten when definitions evolve.
func (h *handlers) ReconcileSiteCustomizationDefaults() error {
	for _, definition := range siteCustomizationDefaults() {
		var existing model.SystemConfig
		err := h.db.Where("config_key = ?", definition.ConfigKey).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := h.db.Create(&definition).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := h.db.Model(&existing).Updates(map[string]interface{}{
				"name":        definition.Name,
				"value_type":  definition.ValueType,
				"description": definition.Description,
				"is_public":   definition.IsPublic,
				"is_secret":   definition.IsSecret,
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
