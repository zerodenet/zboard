package handler

import (
	"errors"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const defaultTermsContent = `# 服务条款

欢迎使用 **{{site_name}}**。访问或使用本服务即表示您同意以下条款。

## 1. 服务说明

{{site_name}} 提供与网络订阅、账户和相关服务管理有关的功能。具体服务内容、价格、流量、速率、设备数量及有效期以购买时展示的信息为准。

## 2. 账户与使用

您应妥善保管账户和订阅凭证，并对账户下发生的使用行为负责。不得利用本服务从事违反适用法律法规、侵害他人权益或影响服务稳定性的行为。

## 3. 计费与续费

订单金额、计费周期、续费条件和可用额度以结算页面及订单记录为准。若服务支持续费，续费后适用当时展示的价格与服务条件。

## 4. 服务变更与可用性

我们可能因维护、安全、供应商变化或不可抗力调整部分服务。对于会实质影响已购服务的变更，我们会通过站点或可用联系方式提供必要说明。

## 5. 终止

违反本条款、适用法律或严重影响平台及其他用户安全时，我们可以限制或终止相关账户或服务。

## 6. 联系方式

如对本条款有疑问，请通过 {{support_email}} 或站点提供的客服入口联系我们。

---

{{copyright}}`

const defaultPrivacyContent = `# 隐私政策

本政策说明 **{{site_name}}** 在提供服务过程中如何处理与您有关的信息。

## 1. 我们处理的信息

为完成注册、登录、订单、订阅交付、客服和安全防护，我们可能处理账户标识、联系方式、订单与订阅记录、必要的设备或访问日志以及您主动提交的信息。

## 2. 使用目的

这些信息主要用于提供和维护服务、处理订单与订阅、保障账户和平台安全、响应客服请求、履行合规义务以及改进服务质量。

## 3. 信息共享

除完成支付、基础设施、通知或法律义务所必需的服务提供方外，我们不会无故向第三方披露您的个人信息。第三方服务的处理活动同时受其自身政策约束。

## 4. 保存与安全

我们根据业务需要和适用法律要求保存必要信息，并采取合理措施降低未经授权访问、泄露、篡改或丢失的风险。

## 5. 您的选择

您可以通过站点提供的账户功能或客服入口查询、更正或提出与个人信息有关的请求，具体范围以适用法律为准。

## 6. 联系方式

隐私相关问题可通过 {{support_email}} 或站点提供的客服入口联系我们。

---

{{copyright}}`

const defaultRefundContent = `# 退款与取消政策

本政策适用于通过 **{{site_name}}** 购买的服务。具体退款资格还可能受到商品说明、支付渠道规则及适用法律的影响。

## 1. 取消未完成订单

尚未完成支付或尚未生效的订单，可在系统允许的情况下直接取消，不产生已支付费用。

## 2. 已生效服务

对于已经生效、已产生流量或已消耗服务资源的订阅，是否支持退款以及可退金额应根据实际使用情况、商品说明和适用法律判断。

## 3. 服务异常

若因平台原因导致已购服务在合理时间内无法交付或持续不可用，请通过客服入口提交订单和问题信息，我们会核实并提供恢复、补偿或退款方案。

## 4. 退款方式

经确认的退款原则上退回原支付渠道。支付渠道产生的处理时间、手续费或汇率差异可能由相应服务商决定。

## 5. 联系方式

退款或取消相关问题可通过 {{support_email}} 或站点提供的客服入口联系我们。

---

{{copyright}}`

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
			ConfigKey:   "site_terms_content",
			Name:        "服务条款",
			Value:       defaultTermsContent,
			ValueType:   "string",
			Description: "服务条款内容。支持 Markdown 与站点变量；若内容仅为一个 HTTP/HTTPS URL，则以前台远端内容模式展示。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_privacy_content",
			Name:        "隐私政策",
			Value:       defaultPrivacyContent,
			ValueType:   "string",
			Description: "隐私政策内容。支持 Markdown 与站点变量；若内容仅为一个 HTTP/HTTPS URL，则以前台远端内容模式展示。",
			IsPublic:    true,
			Revision:    1,
		},
		{
			ConfigKey:   "site_refund_content",
			Name:        "退款政策",
			Value:       defaultRefundContent,
			ValueType:   "string",
			Description: "退款与取消政策内容。支持 Markdown 与站点变量；若内容仅为一个 HTTP/HTTPS URL，则以前台远端内容模式展示。",
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
