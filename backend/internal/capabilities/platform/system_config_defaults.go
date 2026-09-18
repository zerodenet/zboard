package platform

import "context"

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
	return []SystemConfigDefault{
		{Key: "maintenance_enabled", Name: "系统维护模式", Value: "false", ValueType: "bool", Description: "开启后普通用户只能看到维护提示，管理员仍可进入控制台", Public: true},
		{Key: "maintenance_title", Name: "维护页标题", Value: "系统维护中", ValueType: "string", Description: "维护页面显示的标题", Public: true},
		{Key: "maintenance_message", Name: "维护页提示", Value: "系统正在维护，请稍后再试。", ValueType: "string", Description: "维护页面显示的可配置说明", Public: true},
		{Key: "maintenance_task_id", Name: "维护任务标识", Value: "0", ValueType: "int", Description: "由数据库迁移任务维护，不应手动修改"},
		{Key: "subscription_camouflage_url", Name: "订阅伪装跳转地址", Value: "", ValueType: "string", Description: "无效或已撤销的公开订阅链接将跳转到该地址；留空时使用站点公开访问地址"},
		{Key: "register_email_verification", Name: "注册邮箱验证码", Value: "false", ValueType: "bool", Description: "注册时必须先通过邮箱验证码；启用前需完成 SMTP 配置", Public: true},
	}
}
