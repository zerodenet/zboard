package platform

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

type SystemConfigDefault struct {
	Key         string
	Name        string
	Value       string
	ValueType   string
	Description string
	Public      bool
	Secret      bool
}

type SystemConfigDefaultsRepository interface {
	ReconcileSystemConfigDefaults(context.Context, []SystemConfigDefault) error
}

type SystemConfigDefaults struct {
	Repository SystemConfigDefaultsRepository
}

func (s SystemConfigDefaults) Reconcile(ctx context.Context) error {
	return s.Repository.ReconcileSystemConfigDefaults(ctx, DefaultSystemConfigs())
}

func DefaultSystemConfigs() []SystemConfigDefault {
	return append([]SystemConfigDefault{
		{Key: identity.CleanupDaysKey, Name: "无有效订阅账号自动清理天数", Value: "0", ValueType: "int", Description: "0关闭。注册或最后一份订阅结束后连续X天无有效订阅则硬删除账号。管理员、有余额/冻结资金、未结订单、共享订阅或待处理自动续费的账号受保护。历史结算与流量记录保留；开启后立即适用于已有账号。"},
		{Key: SubscriptionPrefixKey, Name: "订阅地址前缀", Value: "", ValueType: "string", Description: "必须由管理员配置，例如 /自定义路径；留空则不提供订阅入口。原默认地址暂时301到此地址，保留token和查询参数；变更后请重新交付已有自定义地址。"},
		{Key: "traffic_report_diagnostics", Name: "旧签名报告诊断采集", Value: "null", ValueType: "json", Description: "默认null关闭；按需填until（RFC3339到期时间，最多24小时）及reason。最多128份待退役正文，每份65535字节，至少7天后回收；余额、计费与原生证明不受开关影响。关闭或满额仍可靠接收计量，只保存摘要回执。"},
		{Key: "maintenance_enabled", Name: "系统维护模式", Value: "false", ValueType: "bool", Description: "开启后普通用户只能看到维护提示，管理员仍可进入控制台", Public: true},
		{Key: "maintenance_title", Name: "维护页标题", Value: "系统维护中", ValueType: "string", Description: "维护页面显示的标题", Public: true},
		{Key: "maintenance_message", Name: "维护页提示", Value: "系统正在维护，请稍后再试。", ValueType: "string", Description: "维护页面显示的可配置说明", Public: true},
		{Key: "maintenance_task_id", Name: "维护任务标识", Value: "0", ValueType: "int", Description: "由数据库迁移任务维护，不应手动修改"},
		{Key: "subscription_camouflage_url", Name: "订阅伪装跳转地址", Value: "", ValueType: "string", Description: "无效或已撤销的公开订阅链接将跳转到该地址；留空时使用站点公开访问地址"},
		{Key: "register_email_verification", Name: "注册邮箱验证码", Value: "false", ValueType: "bool", Description: "注册时必须先通过邮箱验证码；启用前需完成 SMTP 配置", Public: true},
	}, subscriptionAlertDefaults()...)
}
