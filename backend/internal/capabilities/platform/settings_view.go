package platform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type SettingView struct {
	ID          uint               `json:"id"`
	ConfigKey   string             `json:"config_key"`
	Name        string             `json:"name"`
	Value       interface{}        `json:"value,omitempty"`
	ValueType   string             `json:"value_type"`
	Description string             `json:"description"`
	IsPublic    bool               `json:"is_public"`
	IsSecret    bool               `json:"is_secret"`
	Configured  bool               `json:"configured"`
	Revision    uint64             `json:"revision"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Input       SettingInputSchema `json:"input"`
}

type SettingInputOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type SettingInputSchema struct {
	Control     string               `json:"control"`
	Required    bool                 `json:"required,omitempty"`
	Min         *int64               `json:"min,omitempty"`
	Max         *int64               `json:"max,omitempty"`
	Step        *int64               `json:"step,omitempty"`
	MaxBytes    *int                 `json:"max_bytes,omitempty"`
	Placeholder string               `json:"placeholder,omitempty"`
	Options     []SettingInputOption `json:"options,omitempty"`
}

func SettingToView(config Setting) (SettingView, error) {
	view := SettingView{
		ID: config.ID, ConfigKey: config.ConfigKey, Name: config.Name,
		ValueType: config.ValueType, Description: config.Description,
		IsPublic: config.IsPublic, IsSecret: config.IsSecret,
		Configured: config.Configured, Revision: config.Revision, UpdatedAt: config.UpdatedAt,
		Input: SettingInputSchemaFor(config),
	}
	if config.IsSecret {
		return view, nil
	}
	value, err := DecodeSettingValue(config.ValueType, config.Value)
	if err != nil {
		return SettingView{}, fmt.Errorf("decode system config %s: %w", config.ConfigKey, err)
	}
	view.Value = value
	return view, nil
}

func SettingInputSchemaFor(config Setting) SettingInputSchema {
	maxStringBytes := 100000
	schema := SettingInputSchema{Control: "text", MaxBytes: &maxStringBytes}
	switch config.ValueType {
	case "bool":
		schema = SettingInputSchema{Control: "switch"}
	case "int":
		step := int64(1)
		schema = SettingInputSchema{Control: "integer", Step: &step}
	case "json":
		schema = SettingInputSchema{Control: "json"}
	}
	if config.IsSecret {
		schema.Control = "password"
		schema.Placeholder = "输入新值以轮换"
	}

	switch config.ConfigKey {
	case "site_name":
		maxBytes := 80
		schema = SettingInputSchema{Control: "text", Required: true, MaxBytes: &maxBytes, Placeholder: "zboard"}
	case "site_url":
		maxBytes := 255
		schema = SettingInputSchema{Control: "url", Required: true, MaxBytes: &maxBytes, Placeholder: "https://panel.example.com"}
	case "site_desc":
		maxBytes := 500
		schema = SettingInputSchema{Control: "textarea", MaxBytes: &maxBytes, Placeholder: "用于公开页面的简短站点说明"}
	case "site_logo":
		maxBytes := 2048
		schema = SettingInputSchema{Control: "url", MaxBytes: &maxBytes, Placeholder: "https://cdn.example.com/logo.png"}
	case "subscribe_url":
		maxBytes := 2048
		schema = SettingInputSchema{Control: "url", MaxBytes: &maxBytes, Placeholder: "留空时使用站点地址生成"}
	case "subscription_camouflage_url":
		maxBytes := 2048
		schema = SettingInputSchema{Control: "url", MaxBytes: &maxBytes, Placeholder: "留空时跳转到站点公开访问地址"}
	case "task_email_enabled", "register_switch", "register_email_verification", "maintenance_enabled":
		schema = SettingInputSchema{Control: "switch"}
	case "maintenance_title":
		maxBytes := 160
		schema = SettingInputSchema{Control: "text", Required: true, MaxBytes: &maxBytes, Placeholder: "系统维护中"}
	case "maintenance_message":
		maxBytes := 4000
		schema = SettingInputSchema{Control: "textarea", Required: true, MaxBytes: &maxBytes, Placeholder: "说明维护原因和预计恢复时间"}
	case "smtp_host":
		maxBytes := 255
		schema = SettingInputSchema{Control: "hostname", MaxBytes: &maxBytes, Placeholder: "smtp.example.com"}
	case "smtp_port":
		minimum, maximum, step := int64(1), int64(65535), int64(1)
		schema = SettingInputSchema{Control: "port", Min: &minimum, Max: &maximum, Step: &step}
	case "smtp_username":
		maxBytes := 255
		schema = SettingInputSchema{Control: "text", MaxBytes: &maxBytes, Placeholder: "SMTP 登录用户名"}
	case "smtp_password":
		maxBytes := 4096
		schema = SettingInputSchema{Control: "password", MaxBytes: &maxBytes, Placeholder: "输入新密码以轮换"}
	case "smtp_from":
		schema = SettingInputSchema{Control: "email", Placeholder: "noreply@example.com"}
	case "smtp_tls_mode":
		schema = SettingInputSchema{
			Control: "select",
			Options: []SettingInputOption{
				{Label: "STARTTLS（推荐）", Value: "starttls"},
				{Label: "隐式 TLS", Value: "implicit"},
			},
		}
	}
	return schema
}

func DecodeSettingValue(valueType, value string) (interface{}, error) {
	switch valueType {
	case "string":
		return value, nil
	case "bool":
		return strconv.ParseBool(value)
	case "int":
		return strconv.ParseInt(value, 10, 64)
	case "json":
		var decoded interface{}
		if err := json.Unmarshal([]byte(value), &decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported value type %q", valueType)
	}
}
