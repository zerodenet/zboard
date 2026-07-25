package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	taskStatusPending   = int16(0)
	taskStatusRunning   = int16(1)
	taskStatusCompleted = int16(2)
	taskStatusFailed    = int16(3)

	taskTypeQuota          = "quota"
	taskTypeEmail          = "email"
	taskTypeNodeDetect     = "node_detect"
	taskTypeNodeReconcile  = "node_reconcile"
	taskTypeNodeLifecycle  = "node_lifecycle"
	taskTypeProtocolDeploy = "protocol_deploy"
	taskTypeProtocolActive = "protocol_active"
	taskTypeNodeGroupSync  = "node_group_reconcile"

	maxTaskTargets       = 10000
	operationTaskWorkers = 4
)

type systemConfigView struct {
	ID          uint                    `json:"id"`
	ConfigKey   string                  `json:"config_key"`
	Name        string                  `json:"name"`
	Value       interface{}             `json:"value,omitempty"`
	ValueType   string                  `json:"value_type"`
	Description string                  `json:"description"`
	IsPublic    bool                    `json:"is_public"`
	IsSecret    bool                    `json:"is_secret"`
	Configured  bool                    `json:"configured"`
	Revision    uint64                  `json:"revision"`
	UpdatedAt   time.Time               `json:"updated_at"`
	Input       systemConfigInputSchema `json:"input"`
}

type systemConfigInputOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type systemConfigInputSchema struct {
	Control     string                    `json:"control"`
	Required    bool                      `json:"required,omitempty"`
	Min         *int64                    `json:"min,omitempty"`
	Max         *int64                    `json:"max,omitempty"`
	Step        *int64                    `json:"step,omitempty"`
	MaxBytes    *int                      `json:"max_bytes,omitempty"`
	Placeholder string                    `json:"placeholder,omitempty"`
	Options     []systemConfigInputOption `json:"options,omitempty"`
}

type systemConfigUpdateReq struct {
	Value            json.RawMessage `json:"value"`
	ExpectedRevision *uint64         `json:"expected_revision"`
}

type taskScope struct {
	UserIDs         []uint `json:"user_ids,omitempty"`
	SubscriptionIDs []uint `json:"subscription_ids,omitempty"`
	AllActive       bool   `json:"all_active,omitempty"`
}

type taskCreateReq struct {
	Type           string          `json:"type"`
	Scope          taskScope       `json:"scope"`
	Content        json.RawMessage `json:"content"`
	IdempotencyKey string          `json:"idempotency_key"`
	Priority       int             `json:"priority"`
	MaxAttempts    int             `json:"max_attempts"`
	AutoRun        bool            `json:"auto_run"`
}

type quotaTaskContent struct {
	DeltaMB int64  `json:"delta_mb"`
	Reason  string `json:"reason"`
}

type emailTaskContent struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type taskListItem struct {
	model.Task
	PendingCount   int64 `json:"pending_count"`
	RunningCount   int64 `json:"running_count"`
	SucceededCount int64 `json:"succeeded_count"`
	FailedCount    int64 `json:"failed_count"`
}

type taskDetail struct {
	taskListItem
	Items []model.TaskItem `json:"items,omitempty"`
}

type smtpSettings struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string
}

func (h *handlers) PublicSystemConfigsHandler(w http.ResponseWriter, _ *http.Request) {
	var configs []model.SystemConfig
	if err := h.db.Where("is_public = ? AND is_secret = ?", true, false).Order("id asc").Find(&configs).Error; err != nil {
		ServerError(w, err)
		return
	}
	views := make([]systemConfigView, 0, len(configs))
	for _, config := range configs {
		view, err := systemConfigToView(config)
		if err != nil {
			ServerError(w, err)
			return
		}
		views = append(views, view)
	}
	OK(w, views)
}

func (h *handlers) AdminSystemConfigsListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var configs []model.SystemConfig
	if err := h.db.Order("id asc").Find(&configs).Error; err != nil {
		ServerError(w, err)
		return
	}
	views := make([]systemConfigView, 0, len(configs))
	for _, config := range configs {
		view, err := systemConfigToView(config)
		if err != nil {
			ServerError(w, err)
			return
		}
		views = append(views, view)
	}
	OK(w, views)
}

func (h *handlers) AdminSystemConfigUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	key, err := parseConfigKey(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req systemConfigUpdateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if len(req.Value) == 0 {
		BadRequest(w, "value is required")
		return
	}

	var updated model.SystemConfig
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", key).First(&updated).Error; err != nil {
			return err
		}
		if req.ExpectedRevision != nil && updated.Revision != *req.ExpectedRevision {
			return errConfigRevisionConflict
		}
		value, err := normalizeSystemConfigValue(updated, req.Value)
		if err != nil {
			return err
		}
		if err := validateSystemConfigValue(updated.ConfigKey, value); err != nil {
			return err
		}
		storedValue := value
		if updated.IsSecret && value != "" {
			storedValue, err = h.credentialCipher.Encrypt(value)
			if err != nil {
				return err
			}
		}
		updated.Value = storedValue
		updated.Revision++
		if err := tx.Save(&updated).Error; err != nil {
			return err
		}
		if err := syncInstallationConfig(tx, updated.ConfigKey, value); err != nil {
			return err
		}
		if updated.ConfigKey == "task_email_enabled" || strings.HasPrefix(updated.ConfigKey, "smtp_") {
			if _, err := h.loadSMTPSettings(tx, false); err != nil {
				return err
			}
		}
		return createAuditLog(tx, claims, "system.config.update", "system_config:"+updated.ConfigKey, fmt.Sprintf("revision=%d", updated.Revision))
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errConfigRevisionConflict) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	view, err := systemConfigToView(updated)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, view)
}

var errConfigRevisionConflict = errors.New("system config revision conflict")

func parseConfigKey(path string) (string, error) {
	const prefix = "/api/v1/admin/system-configs/"
	key := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if key == "" || strings.Contains(key, "/") || len(key) > 80 {
		return "", errors.New("invalid system config key")
	}
	for _, ch := range key {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return "", errors.New("invalid system config key")
		}
	}
	return key, nil
}

func systemConfigToView(config model.SystemConfig) (systemConfigView, error) {
	view := systemConfigView{
		ID: config.ID, ConfigKey: config.ConfigKey, Name: config.Name,
		ValueType: config.ValueType, Description: config.Description,
		IsPublic: config.IsPublic, IsSecret: config.IsSecret,
		Configured: config.Value != "", Revision: config.Revision, UpdatedAt: config.UpdatedAt,
		Input: systemConfigInputSchemaFor(config),
	}
	if config.IsSecret {
		return view, nil
	}
	value, err := decodeSystemConfigValue(config.ValueType, config.Value)
	if err != nil {
		return systemConfigView{}, fmt.Errorf("decode system config %s: %w", config.ConfigKey, err)
	}
	view.Value = value
	return view, nil
}

func systemConfigInputSchemaFor(config model.SystemConfig) systemConfigInputSchema {
	maxStringBytes := 100000
	schema := systemConfigInputSchema{Control: "text", MaxBytes: &maxStringBytes}
	switch config.ValueType {
	case "bool":
		schema = systemConfigInputSchema{Control: "switch"}
	case "int":
		step := int64(1)
		schema = systemConfigInputSchema{Control: "integer", Step: &step}
	case "json":
		schema = systemConfigInputSchema{Control: "json"}
	}
	if config.IsSecret {
		schema.Control = "password"
		schema.Placeholder = "输入新值以轮换"
	}

	switch config.ConfigKey {
	case "site_name":
		maxBytes := 80
		schema = systemConfigInputSchema{Control: "text", Required: true, MaxBytes: &maxBytes, Placeholder: "zboard"}
	case "site_url":
		maxBytes := 2048
		schema = systemConfigInputSchema{Control: "url", Required: true, MaxBytes: &maxBytes, Placeholder: "https://panel.example.com"}
	case "site_desc":
		maxBytes := 500
		schema = systemConfigInputSchema{Control: "textarea", MaxBytes: &maxBytes, Placeholder: "用于公开页面的简短站点说明"}
	case "site_logo":
		maxBytes := 2048
		schema = systemConfigInputSchema{Control: "url", MaxBytes: &maxBytes, Placeholder: "https://cdn.example.com/logo.png"}
	case "subscribe_url":
		maxBytes := 2048
		schema = systemConfigInputSchema{Control: "url", MaxBytes: &maxBytes, Placeholder: "留空时使用站点地址生成"}
	case "task_email_enabled", "register_switch":
		schema = systemConfigInputSchema{Control: "switch"}
	case "smtp_host":
		maxBytes := 255
		schema = systemConfigInputSchema{Control: "hostname", MaxBytes: &maxBytes, Placeholder: "smtp.example.com"}
	case "smtp_port":
		minimum, maximum, step := int64(1), int64(65535), int64(1)
		schema = systemConfigInputSchema{Control: "port", Min: &minimum, Max: &maximum, Step: &step}
	case "smtp_username":
		maxBytes := 255
		schema = systemConfigInputSchema{Control: "text", MaxBytes: &maxBytes, Placeholder: "SMTP 登录用户名"}
	case "smtp_password":
		maxBytes := 4096
		schema = systemConfigInputSchema{Control: "password", MaxBytes: &maxBytes, Placeholder: "输入新密码以轮换"}
	case "smtp_from":
		schema = systemConfigInputSchema{Control: "email", Placeholder: "noreply@example.com"}
	case "smtp_tls_mode":
		schema = systemConfigInputSchema{
			Control: "select",
			Options: []systemConfigInputOption{
				{Label: "STARTTLS（推荐）", Value: "starttls"},
				{Label: "隐式 TLS", Value: "implicit"},
			},
		}
	}
	return schema
}

func decodeSystemConfigValue(valueType, value string) (interface{}, error) {
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

func normalizeSystemConfigValue(config model.SystemConfig, raw json.RawMessage) (string, error) {
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

func validateSystemConfigValue(key, value string) error {
	switch key {
	case "site_name":
		if value == "" || len(value) > 80 {
			return errors.New("site_name must contain 1 to 80 bytes")
		}
	case "site_url", "subscribe_url", "site_logo":
		if value == "" && key != "site_url" {
			return nil
		}
		if len(value) > 2048 {
			return fmt.Errorf("%s must not exceed 2048 bytes", key)
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
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
		if value != "" && !validEmail(value) {
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
	}
	return nil
}

func syncInstallationConfig(tx *gorm.DB, key, value string) error {
	updates := map[string]interface{}{}
	switch key {
	case "site_name":
		updates["site_name"] = value
	case "site_url":
		updates["site_url"] = value
	case "register_switch":
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		updates["allow_registration"] = enabled
	default:
		return nil
	}
	return tx.Model(&model.Installation{}).Where("id = ?", 1).Updates(updates).Error
}

func (h *handlers) AdminTasksListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	limit := queryInt(r, "limit", 50, 1, 100)
	offset := queryInt(r, "offset", 0, 0, 1000000)
	query := h.db.Model(&model.Task{})
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		status, err := strconv.ParseInt(raw, 10, 16)
		if err != nil || status < 0 || status > 3 {
			BadRequest(w, "status must be between 0 and 3")
			return
		}
		query = query.Where("status = ?", status)
	}
	if taskType := strings.TrimSpace(r.URL.Query().Get("type")); taskType != "" {
		query = query.Where("type = ?", taskType)
	}
	paged := r.URL.Query().Get("paged") == "true"
	var total int64
	if paged {
		if err := query.Count(&total).Error; err != nil {
			ServerError(w, err)
			return
		}
	}
	var tasks []model.Task
	if err := query.Order("priority desc, id desc").Limit(limit).Offset(offset).Find(&tasks).Error; err != nil {
		ServerError(w, err)
		return
	}
	views, err := h.buildTaskListItems(tasks)
	if err != nil {
		ServerError(w, err)
		return
	}
	if paged {
		OK(w, pagedData(views, total, offset, limit))
		return
	}
	OK(w, views)
}

func (h *handlers) AdminTaskGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/tasks/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var task model.Task
	if err := h.db.First(&task, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	} else if err != nil {
		ServerError(w, err)
		return
	}
	views, err := h.buildTaskListItems([]model.Task{task})
	if err != nil {
		ServerError(w, err)
		return
	}
	view := views[0]
	if parseBoolQuery(r.URL.Query().Get("summary")) {
		OK(w, view)
		return
	}
	var items []model.TaskItem
	if err := h.db.Where("task_id = ?", task.ID).Order("id asc").Limit(maxTaskTargets).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, taskDetail{taskListItem: view, Items: items})
}

func (h *handlers) buildTaskListItems(tasks []model.Task) ([]taskListItem, error) {
	views := make([]taskListItem, 0, len(tasks))
	if len(tasks) == 0 {
		return views, nil
	}
	ids := make([]uint, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	type countRow struct {
		TaskID         uint
		PendingCount   int64
		RunningCount   int64
		SucceededCount int64
		FailedCount    int64
	}
	rows := make([]countRow, 0, len(tasks))
	if err := h.db.Model(&model.TaskItem{}).
		Select("task_id, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS pending_count, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS running_count, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS succeeded_count, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS failed_count", taskStatusPending, taskStatusRunning, taskStatusCompleted, taskStatusFailed).
		Where("task_id IN ?", ids).Group("task_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[uint]countRow, len(rows))
	for _, row := range rows {
		counts[row.TaskID] = row
	}
	for _, task := range tasks {
		count := counts[task.ID]
		views = append(views, taskListItem{Task: task, PendingCount: count.PendingCount, RunningCount: count.RunningCount, SucceededCount: count.SucceededCount, FailedCount: count.FailedCount})
	}
	return views, nil
}

func (h *handlers) AdminTaskItemsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/tasks/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var exists int64
	if err := h.db.Model(&model.Task{}).Where("id = ?", id).Count(&exists).Error; err != nil {
		ServerError(w, err)
		return
	}
	if exists == 0 {
		NotFound(w)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.TaskItem{}).Where("task_id = ?", id)
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		status, parseErr := strconv.ParseInt(raw, 10, 16)
		if parseErr != nil || status < int64(taskStatusPending) || status > int64(taskStatusFailed) {
			BadRequest(w, "status must be between 0 and 3")
			return
		}
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.TaskItem, 0, limit)
	if err := query.Order("id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(items, total, offset, limit))
}

func (h *handlers) AdminTaskCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req taskCreateReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	task, items, err := h.prepareTask(req)
	if err != nil {
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
			return
		}
		ServerError(w, err)
		return
	}
	task, err = h.persistAdminTask(claims, task, items, req.AutoRun)
	if err != nil {
		if isDuplicateError(err) {
			writeJSON(w, http.StatusConflict, "task idempotency key already exists", nil)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, task)
}

func (h *handlers) persistAdminTask(claims authClaims, task model.Task, items []model.TaskItem, autoRun bool) (model.Task, error) {
	err := h.db.Transaction(func(tx *gorm.DB) error {
		return persistAdminTaskRecords(tx, claims, &task, items)
	})
	if err != nil {
		return model.Task{}, err
	}
	if autoRun {
		if err := h.startPersistedAdminTask(&task); err != nil {
			return task, err
		}
	}
	return task, nil
}

func persistAdminTaskRecords(tx *gorm.DB, claims authClaims, task *model.Task, items []model.TaskItem) error {
	if err := tx.Create(task).Error; err != nil {
		return err
	}
	for i := range items {
		items[i].TaskID = task.ID
	}
	if err := tx.CreateInBatches(items, 250).Error; err != nil {
		return err
	}
	return createAuditLog(tx, claims, "task.create", fmt.Sprintf("task:%d", task.ID), fmt.Sprintf("type=%s total=%d", task.Type, task.Total))
}

func (h *handlers) startPersistedAdminTask(task *model.Task) error {
	lockID, err := h.claimTask(task.ID, nil)
	if err != nil {
		return err
	}
	task.Status = taskStatusRunning
	task.Attempts++
	go h.executeClaimedTask(task.ID, lockID)
	return nil
}

func (h *handlers) AdminTaskRunHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseTaskRunID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	lockID, err := h.claimTask(id, &claims)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	go h.executeClaimedTask(id, lockID)
	writeJSON(w, http.StatusAccepted, "task execution started", map[string]interface{}{"id": id, "status": taskStatusRunning})
}

func (h *handlers) prepareTask(req taskCreateReq) (model.Task, []model.TaskItem, error) {
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	fields := map[string]string{}
	if req.Type != taskTypeQuota && req.Type != taskTypeEmail {
		fields["type"] = "请选择配额调整或邮件通知任务。"
	}
	if len(req.Content) == 0 || !json.Valid(req.Content) {
		fields["content"] = "任务内容格式无效，请重新填写。"
	}
	if req.MaxAttempts == 0 {
		req.MaxAttempts = 3
	}
	if req.MaxAttempts < 1 || req.MaxAttempts > 10 {
		fields["max_attempts"] = "最大尝试次数必须在 1 到 10 之间。"
	}
	if req.Priority < -100 || req.Priority > 100 {
		fields["priority"] = "任务优先级必须在 -100 到 100 之间。"
	}
	if len(fields) > 0 {
		return model.Task{}, nil, validationError("任务信息校验失败。", fields)
	}
	req.Scope.UserIDs = uniqueUintIDs(req.Scope.UserIDs)
	req.Scope.SubscriptionIDs = uniqueUintIDs(req.Scope.SubscriptionIDs)
	if err := validateTaskContent(req.Type, req.Content); err != nil {
		return model.Task{}, nil, err
	}
	items, err := h.resolveTaskItems(req.Type, req.Scope)
	if err != nil {
		return model.Task{}, nil, err
	}
	if len(items) == 0 {
		return model.Task{}, nil, validationError("任务范围校验失败。", map[string]string{"scope": "当前范围没有可执行的有效目标，请调整后重试。"})
	}
	if len(items) > maxTaskTargets {
		return model.Task{}, nil, validationError("任务范围校验失败。", map[string]string{"scope": fmt.Sprintf("单个任务最多支持 %d 个目标，请缩小范围。", maxTaskTargets)})
	}
	scopeJSON, err := json.Marshal(req.Scope)
	if err != nil {
		return model.Task{}, nil, err
	}
	var canonicalContent interface{}
	if err := json.Unmarshal(req.Content, &canonicalContent); err != nil {
		return model.Task{}, nil, err
	}
	contentJSON, err := json.Marshal(canonicalContent)
	if err != nil {
		return model.Task{}, nil, err
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	if len(idempotencyKey) > 128 {
		return model.Task{}, nil, validationError("任务信息校验失败。", map[string]string{"idempotency_key": "幂等键不能超过 128 个字符。"})
	}
	task := model.Task{
		Type: req.Type, Scope: string(scopeJSON), Content: string(contentJSON),
		Status: taskStatusPending, Total: int64(len(items)), IdempotencyKey: idempotencyKey,
		Priority: req.Priority, MaxAttempts: req.MaxAttempts,
	}
	return task, items, nil
}

func validateTaskContent(taskType string, raw json.RawMessage) error {
	switch taskType {
	case taskTypeQuota:
		var content quotaTaskContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return validationError("任务内容校验失败。", map[string]string{"content": "配额调整内容格式无效。"})
		}
		fields := map[string]string{}
		if content.DeltaMB == 0 || content.DeltaMB < -1000000000 || content.DeltaMB > 1000000000 {
			fields["content.delta_mb"] = "调整量必须为非零值，且在 -1000000000 到 1000000000 MB 之间。"
		}
		if len(strings.TrimSpace(content.Reason)) < 3 || len(content.Reason) > 255 {
			fields["content.reason"] = "调整原因需包含 3 到 255 个字符。"
		}
		if len(fields) > 0 {
			return validationError("任务内容校验失败。", fields)
		}
	case taskTypeEmail:
		var content emailTaskContent
		if err := json.Unmarshal(raw, &content); err != nil {
			return validationError("任务内容校验失败。", map[string]string{"content": "邮件内容格式无效。"})
		}
		fields := map[string]string{}
		content.Subject = strings.TrimSpace(content.Subject)
		if content.Subject == "" || len(content.Subject) > 200 || strings.ContainsAny(content.Subject, "\r\n") {
			fields["content.subject"] = "邮件主题需包含 1 到 200 个字符，且不能换行。"
		}
		if strings.TrimSpace(content.Body) == "" || len(content.Body) > 100000 {
			fields["content.body"] = "邮件正文需包含 1 到 100000 个字符。"
		}
		if len(fields) > 0 {
			return validationError("任务内容校验失败。", fields)
		}
	}
	return nil
}

func (h *handlers) resolveTaskItems(taskType string, scope taskScope) ([]model.TaskItem, error) {
	switch taskType {
	case taskTypeQuota:
		query := h.db.Model(&model.Subscription{}).Select("id").Where("end_at > ?", time.Now().UTC())
		query = applyTaskScope(query, scope, true)
		var ids []uint
		if err := query.Limit(maxTaskTargets+1).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		items := make([]model.TaskItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, newTaskItem("subscription", id))
		}
		return items, nil
	case taskTypeEmail:
		query := h.db.Model(&model.User{}).Select("id").Where("status = ?", userStatusActive)
		query = applyTaskScope(query, scope, false)
		var ids []uint
		if err := query.Limit(maxTaskTargets+1).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		items := make([]model.TaskItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, newTaskItem("user", id))
		}
		return items, nil
	default:
		return nil, errors.New("unsupported task type")
	}
}

func newTaskItem(targetType string, targetID uint) model.TaskItem {
	return model.TaskItem{
		TargetType: targetType,
		TargetID:   strconv.FormatUint(uint64(targetID), 10),
		Payload:    "{}",
		Status:     taskStatusPending,
	}
}

func applyTaskScope(query *gorm.DB, scope taskScope, subscription bool) *gorm.DB {
	if scope.AllActive {
		return query
	}
	if subscription {
		switch {
		case len(scope.SubscriptionIDs) > 0 && len(scope.UserIDs) > 0:
			return query.Where("id IN ? OR user_id IN ?", scope.SubscriptionIDs, scope.UserIDs)
		case len(scope.SubscriptionIDs) > 0:
			return query.Where("id IN ?", scope.SubscriptionIDs)
		case len(scope.UserIDs) > 0:
			return query.Where("user_id IN ?", scope.UserIDs)
		default:
			return query.Where("1 = 0")
		}
	}
	if len(scope.UserIDs) == 0 {
		return query.Where("1 = 0")
	}
	return query.Where("id IN ?", scope.UserIDs)
}

func (h *handlers) claimTask(id uint, claims *authClaims) (string, error) {
	lockID := uuid.NewString()
	now := time.Now().UTC()
	return lockID, h.db.Transaction(func(tx *gorm.DB) error {
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
			return err
		}
		if task.Status == taskStatusCompleted {
			return errors.New("completed task cannot run again")
		}
		if task.Status == taskStatusRunning && (task.LockedUntil == nil || task.LockedUntil.After(now)) {
			return errors.New("task is already running")
		}
		if task.Attempts >= task.MaxAttempts {
			return errors.New("task has reached max_attempts")
		}
		if claims != nil {
			if err := createAuditLog(tx, *claims, "task.run", fmt.Sprintf("task:%d", id), "queued"); err != nil {
				return err
			}
		}
		var completed int64
		if err := tx.Model(&model.TaskItem{}).Where("task_id = ? AND status = ?", task.ID, taskStatusCompleted).Count(&completed).Error; err != nil {
			return err
		}
		lockedUntil := now.Add(30 * time.Minute)
		return tx.Model(&task).Updates(map[string]interface{}{
			"status": taskStatusRunning, "errors": "", "started_at": now, "finished_at": nil,
			"attempts": gorm.Expr("attempts + 1"), "current": completed, "locked_by": lockID, "locked_until": lockedUntil,
		}).Error
	})
}

func (h *handlers) executeClaimedTask(taskID uint, lockID string) {
	var task model.Task
	if err := h.db.Where("id = ? AND status = ? AND locked_by = ?", taskID, taskStatusRunning, lockID).First(&task).Error; err != nil {
		return
	}
	var items []model.TaskItem
	if err := h.db.Where("task_id = ? AND status IN ?", task.ID, []int16{taskStatusPending, taskStatusRunning, taskStatusFailed}).Order("id asc").Find(&items).Error; err != nil {
		h.finishTask(task, lockID, []string{err.Error()})
		return
	}
	stopHeartbeat := make(chan struct{})
	go h.refreshTaskLock(task.ID, lockID, stopHeartbeat)
	errorsSeen := h.executeTaskItems(task, items, lockID)
	close(stopHeartbeat)
	h.finishTask(task, lockID, errorsSeen)
}

func isOperationTaskType(taskType string) bool {
	switch taskType {
	case taskTypeNodeDetect, taskTypeNodeReconcile, taskTypeNodeLifecycle, taskTypeProtocolDeploy, taskTypeProtocolActive:
		return true
	default:
		return false
	}
}

func (h *handlers) executeTaskItems(task model.Task, items []model.TaskItem, lockID string) []string {
	results := make([][]string, len(items))
	start := 0
	if task.Type == taskTypeNodeGroupSync && len(items) > 0 && items[0].TargetType == "node_group" {
		results[0] = h.executeClaimedTaskItem(task, &items[0], lockID)
		if len(results[0]) > 0 {
			return results[0]
		}
		start = 1
	}
	if (!isOperationTaskType(task.Type) && task.Type != taskTypeNodeGroupSync) || len(items)-start <= 1 {
		for i := start; i < len(items); i++ {
			results[i] = h.executeClaimedTaskItem(task, &items[i], lockID)
		}
	} else {
		h.executeTaskItemRangeParallel(task, items, results, start, lockID)
	}
	errorsSeen := make([]string, 0)
	for _, itemErrors := range results {
		errorsSeen = append(errorsSeen, itemErrors...)
	}
	return errorsSeen
}

func (h *handlers) executeTaskItemRangeParallel(task model.Task, items []model.TaskItem, results [][]string, start int, lockID string) {
	remaining := len(items) - start
	if remaining <= 0 {
		return
	}
	workerCount := operationTaskWorkers
	if remaining < workerCount {
		workerCount = remaining
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = h.executeClaimedTaskItem(task, &items[index], lockID)
			}
		}()
	}
	for index := start; index < len(items); index++ {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
}

func (h *handlers) executeClaimedTaskItem(task model.Task, item *model.TaskItem, lockID string) []string {
	errorsSeen := make([]string, 0, 2)
	now := time.Now().UTC()
	if err := h.db.Model(item).Updates(map[string]interface{}{
		"status": taskStatusRunning, "attempts": gorm.Expr("attempts + 1"), "error": "", "started_at": now, "finished_at": nil,
	}).Error; err != nil {
		return append(errorsSeen, fmt.Sprintf("item %d: %v", item.ID, err))
	}
	err := h.executeTaskItem(task, *item)
	finishedAt := time.Now().UTC()
	status := taskStatusCompleted
	errorText := ""
	if err != nil {
		status = taskStatusFailed
		errorText = truncateTaskError(err.Error())
		errorsSeen = append(errorsSeen, fmt.Sprintf("item %d: %s", item.ID, errorText))
	}
	if err := h.db.Model(item).Updates(map[string]interface{}{
		"status": status, "error": errorText, "finished_at": finishedAt,
	}).Error; err != nil {
		return append(errorsSeen, fmt.Sprintf("item %d status update: %v", item.ID, err))
	}
	if err := h.db.Model(&model.Task{}).Where("id = ? AND locked_by = ?", task.ID, lockID).Update("current", gorm.Expr("current + 1")).Error; err != nil {
		errorsSeen = append(errorsSeen, fmt.Sprintf("task %d progress update: %v", task.ID, err))
	}
	return errorsSeen
}

func (h *handlers) refreshTaskLock(taskID uint, lockID string, stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			lockedUntil := time.Now().UTC().Add(30 * time.Minute)
			result := h.db.Model(&model.Task{}).Where("id = ? AND status = ? AND locked_by = ?", taskID, taskStatusRunning, lockID).Update("locked_until", lockedUntil)
			if result.Error != nil || result.RowsAffected == 0 {
				return
			}
		case <-stop:
			return
		}
	}
}

func (h *handlers) finishTask(task model.Task, lockID string, errorsSeen []string) {
	status := taskStatusCompleted
	if len(errorsSeen) > 0 {
		status = taskStatusFailed
	}
	if len(errorsSeen) > 20 {
		errorsSeen = errorsSeen[:20]
	}
	now := time.Now().UTC()
	_ = h.db.Model(&model.Task{}).Where("id = ? AND locked_by = ?", task.ID, lockID).Updates(map[string]interface{}{
		"status": status, "errors": strings.Join(errorsSeen, "\n"), "finished_at": now,
		"locked_by": "", "locked_until": nil,
	}).Error
}

func (h *handlers) executeTaskItem(task model.Task, item model.TaskItem) error {
	switch task.Type {
	case taskTypeQuota:
		return h.executeQuotaTaskItem(task, item)
	case taskTypeEmail:
		return h.executeEmailTaskItem(task, item)
	case taskTypeNodeDetect, taskTypeNodeReconcile, taskTypeNodeLifecycle, taskTypeProtocolDeploy, taskTypeProtocolActive, taskTypeNodeGroupSync:
		return h.executeOperationTaskItem(task, item)
	default:
		return errors.New("unsupported task type")
	}
}

func (h *handlers) executeQuotaTaskItem(task model.Task, item model.TaskItem) error {
	subscriptionID, err := strconv.ParseUint(item.TargetID, 10, 64)
	if err != nil || subscriptionID == 0 {
		return errors.New("invalid subscription target")
	}
	var content quotaTaskContent
	if err := json.Unmarshal([]byte(task.Content), &content); err != nil {
		return err
	}
	deltaBytes := content.DeltaMB * 1024 * 1024
	return h.db.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&model.QuotaEvent{}).Where(
			"subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?",
			subscriptionID, "task_adjustment", "task_item", strconv.FormatUint(uint64(item.ID), 10),
		).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		var subscription model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&subscription, uint(subscriptionID)).Error; err != nil {
			return err
		}
		if (deltaBytes > 0 && subscription.FlowTotal > int64(^uint64(0)>>1)-deltaBytes) ||
			(deltaBytes < 0 && subscription.FlowTotal < -deltaBytes) {
			return errors.New("quota adjustment overflows subscription total")
		}
		newTotal := subscription.FlowTotal + deltaBytes
		if newTotal < subscription.FlowUsed {
			return errors.New("quota adjustment would reduce total below used traffic")
		}
		before := subscription.FlowTotal - subscription.FlowUsed
		updates := map[string]interface{}{"flow_total": newTotal}
		if subscription.EndAt.After(time.Now().UTC()) && newTotal > subscription.FlowUsed {
			updates["status"] = subStatusActive
		}
		if err := tx.Model(&subscription).Updates(updates).Error; err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]interface{}{"reason": strings.TrimSpace(content.Reason), "task_id": task.ID})
		return tx.Create(&model.QuotaEvent{
			SubscriptionID: subscription.ID, EventType: "task_adjustment", DeltaBytes: deltaBytes,
			BalanceBefore: before, BalanceAfter: newTotal - subscription.FlowUsed,
			ReferenceType: "task_item", ReferenceID: strconv.FormatUint(uint64(item.ID), 10), Detail: string(detail),
		}).Error
	})
}

func (h *handlers) executeEmailTaskItem(task model.Task, item model.TaskItem) error {
	userID, err := strconv.ParseUint(item.TargetID, 10, 64)
	if err != nil || userID == 0 {
		return errors.New("invalid user target")
	}
	var user model.User
	if err := h.db.Where("id = ? AND status = ?", userID, userStatusActive).First(&user).Error; err != nil {
		return err
	}
	if !validEmail(user.Email) {
		return errors.New("target user email is invalid")
	}
	var content emailTaskContent
	if err := json.Unmarshal([]byte(task.Content), &content); err != nil {
		return err
	}
	settings, err := h.loadSMTPSettings(h.db, true)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	messageID := fmt.Sprintf("<task-%d-item-%d@%s>", task.ID, item.ID, settings.Host)
	return sendSMTPMail(ctx, settings, user.Email, content.Subject, content.Body, messageID)
}

func (h *handlers) loadSMTPSettings(db *gorm.DB, requireEnabled bool) (smtpSettings, error) {
	keys := []string{"task_email_enabled", "smtp_host", "smtp_port", "smtp_username", "smtp_password", "smtp_from", "smtp_tls_mode"}
	var configs []model.SystemConfig
	if err := db.Where("config_key IN ?", keys).Find(&configs).Error; err != nil {
		return smtpSettings{}, err
	}
	values := make(map[string]string, len(configs))
	for _, config := range configs {
		value := config.Value
		if config.IsSecret && value != "" {
			decrypted, err := h.credentialCipher.Decrypt(value)
			if err != nil {
				return smtpSettings{}, fmt.Errorf("decrypt %s: %w", config.ConfigKey, err)
			}
			value = decrypted
		}
		values[config.ConfigKey] = value
	}
	port, err := strconv.Atoi(values["smtp_port"])
	if err != nil {
		return smtpSettings{}, errors.New("smtp_port is invalid")
	}
	enabled, _ := strconv.ParseBool(values["task_email_enabled"])
	settings := smtpSettings{
		Enabled: enabled, Host: values["smtp_host"], Port: port,
		Username: values["smtp_username"], Password: values["smtp_password"],
		From: values["smtp_from"], TLSMode: values["smtp_tls_mode"],
	}
	if !settings.Enabled && !requireEnabled {
		return settings, nil
	}
	if requireEnabled && !settings.Enabled {
		return smtpSettings{}, errors.New("email tasks are disabled")
	}
	if settings.Host == "" || settings.Port < 1 || settings.Port > 65535 || !validEmail(settings.From) {
		return smtpSettings{}, errors.New("SMTP host, port and from address must be configured")
	}
	if settings.TLSMode != "starttls" && settings.TLSMode != "implicit" {
		return smtpSettings{}, errors.New("smtp_tls_mode must be starttls or implicit")
	}
	if settings.Username != "" && settings.Password == "" {
		return smtpSettings{}, errors.New("smtp_password is required when smtp_username is configured")
	}
	return settings, nil
}

func sendSMTPMail(ctx context.Context, settings smtpSettings, recipient, subject, body, messageID string) error {
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	tlsConfig := &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12}
	if settings.TLSMode == "implicit" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	client, err := smtp.NewClient(conn, settings.Host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()
	if settings.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if settings.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.Username, settings.Password, settings.Host)); err != nil {
			return fmt.Errorf("authenticate SMTP client: %w", err)
		}
	}
	if err := client.Mail(settings.From); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	message := buildSMTPMessage(settings.From, recipient, subject, body, messageID)
	if _, err := wc.Write([]byte(message)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit SMTP session: %w", err)
	}
	return nil
}

func buildSMTPMessage(from, recipient, subject, body, messageID string) string {
	encodedSubject := mime.QEncoding.Encode("UTF-8", strings.TrimSpace(subject))
	return "From: " + from + "\r\n" +
		"To: " + recipient + "\r\n" +
		"Subject: " + encodedSubject + "\r\n" +
		"Message-ID: " + messageID + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + body
}

func parseTaskRunID(path string) (uint, error) {
	const suffix = "/run"
	if !strings.HasSuffix(strings.TrimRight(path, "/"), suffix) {
		return 0, errors.New("invalid task run path")
	}
	normalized := strings.TrimSuffix(strings.TrimRight(path, "/"), suffix)
	return parsePathID(normalized, "/api/v1/admin/tasks/")
}

func queryInt(r *http.Request, key string, fallback, minimum, maximum int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return fallback
	}
	return value
}

func truncateTaskError(value string) string {
	const limit = 2000
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
