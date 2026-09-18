package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	smtpadapter "github.com/zerodenet/zboard/backend/internal/adapters/smtp"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	taskStatusPending   = int16(0)
	taskStatusRunning   = int16(1)
	taskStatusCompleted = int16(2)
	taskStatusFailed    = int16(3)

	taskTypeQuota             = "quota"
	taskTypeEmail             = "email"
	taskTypeNodeDetect        = network.BatchNodeDetect
	taskTypeNodeReconcile     = network.BatchNodeReconcile
	taskTypeNodeLifecycle     = network.BatchNodeLifecycle
	taskTypeProtocolDeploy    = network.BatchProtocolDeploy
	taskTypeProtocolActive    = network.BatchProtocolActive
	taskTypeNodeGroupSync     = network.BatchNodeGroupSync
	taskTypeDatabaseMigration = "database_migration"

	maxTaskTargets       = 10000
	operationTaskWorkers = 1
)

type systemConfigView = platform.SettingView
type systemConfigInputSchema = platform.SettingInputSchema

func (h *handlers) ReconcileSystemConfigDefaults() error {
	return h.services.SystemConfigDefaults.Reconcile(context.Background())
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

type emailTaskContent = messaging.EmailContent

type smtpSettings = platform.SMTPSettings

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
	if key == "maintenance_task_id" {
		Forbidden(w, "maintenance_task_id is managed by database migration tasks")
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

	if key == "maintenance_enabled" || key == "maintenance_title" || key == "maintenance_message" {
		h.updateMaintenanceSetting(w, r, claims, key, req)
		return
	}
	view, err := h.services.SettingUpdate(h.credentialCipher).Update(r.Context(), claims.UserID, platform.SettingUpdateInput{Key: key, Value: req.Value, ExpectedRevision: req.ExpectedRevision})
	if errors.Is(err, platform.ErrSettingNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, platform.ErrSettingRevision) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if errors.Is(err, platform.ErrSettingsPermission) {
		Forbidden(w, "settings administrator authorization changed")
		return
	}
	if errors.Is(err, platform.ErrMaintenanceBusy) {
		ServiceUnavailable(w, "database migration locks settings changes")
		return
	}
	var invalid *platform.SettingValidation
	if errors.As(err, &invalid) {
		BadRequest(w, invalid.Error())
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "system config update failed", nil)
		return
	}
	OK(w, view)
}

var errConfigRevisionConflict = platform.ErrSettingRevision

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
	return platform.SettingToView(settingRecord(config))
}
func systemConfigInputSchemaFor(config model.SystemConfig) systemConfigInputSchema {
	return platform.SettingInputSchemaFor(settingRecord(config))
}
func settingRecord(config model.SystemConfig) platform.Setting {
	return platform.Setting{ID: config.ID, ConfigKey: config.ConfigKey, Name: config.Name, Value: config.Value, ValueType: config.ValueType, Description: config.Description, IsPublic: config.IsPublic, IsSecret: config.IsSecret, Configured: config.Value != "", Revision: config.Revision, UpdatedAt: config.UpdatedAt}
}

func normalizeSystemConfigValue(config model.SystemConfig, raw json.RawMessage) (string, error) {
	return platform.NormalizeSettingValue(settingRecord(config), raw)
}
func validateSystemConfigValue(key, value string) error {
	return platform.ValidateSettingValue(key, value)
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
	if strings.EqualFold(strings.TrimSpace(req.Type), taskTypeEmail) {
		h.createMailRequest(w, r, claims, req)
		return
	}
	if strings.EqualFold(strings.TrimSpace(req.Type), taskTypeQuota) {
		h.createQuotaRequest(w, r, claims, req)
		return
	}
	BadRequest(w, "请选择配额调整或邮件通知任务。")
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
	err = h.services.BatchControl().Queue(r.Context(), claims.UserID, id)
	if errors.Is(err, jobs.ErrBatchPermission) {
		Forbidden(w, "current administrator required")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	h.StartAdminTaskWorker()
	writeJSON(w, http.StatusAccepted, "task queued", map[string]interface{}{"id": id, "status": taskStatusPending})
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

func newTaskItem(targetType string, targetID uint) model.TaskItem {
	return model.TaskItem{
		TargetType: targetType,
		TargetID:   strconv.FormatUint(uint64(targetID), 10),
		Payload:    "{}",
		Status:     taskStatusPending,
	}
}

func (h *handlers) claimTask(id uint, claims *authClaims) (string, error) {
	return h.claimTaskMode(id, claims, false)
}

func (h *handlers) claimTaskMode(id uint, claims *authClaims, queuedOnly bool) (string, error) {
	var actor uint
	if claims != nil {
		actor = claims.UserID
	}
	return h.services.ClaimBatch(context.Background(), actor, id, queuedOnly)
}

func (h *handlers) executeClaimedTask(taskID uint, lockID string) error {
	return h.executeClaimedTaskContext(context.Background(), taskID, lockID)
}
func (h *handlers) executeClaimedTaskContext(ctx context.Context, taskID uint, lockID string) error {
	return h.services.BatchRunner(h).Run(ctx, taskID, lockID)
}

func isOperationTaskType(taskType string) bool {
	switch taskType {
	case taskTypeNodeDetect, taskTypeNodeReconcile, taskTypeNodeLifecycle, taskTypeProtocolDeploy, taskTypeProtocolActive:
		return true
	default:
		return false
	}
}

// A batch owns one admitted execution slot. Child operations stay sequential;
// parallel work must become independent intents admitted by the common runtime.
func (h *handlers) executeTaskItems(task model.Task, items []model.TaskItem, lockID string) []string {
	return h.executeTaskItemsContext(context.Background(), task, items, lockID)
}
func (h *handlers) executeTaskItemsContext(ctx context.Context, task model.Task, items []model.TaskItem, lockID string) []string {
	errorsSeen := []string{}
	for i := range items {
		if err := ctx.Err(); err != nil {
			return append(errorsSeen, err.Error())
		}
		failures := h.executeClaimedTaskItemContext(ctx, task, &items[i], lockID)
		errorsSeen = append(errorsSeen, failures...)
		if i == 0 && task.Type == taskTypeNodeGroupSync && items[i].TargetType == "node_group" && len(failures) > 0 {
			return errorsSeen
		}
	}
	return errorsSeen
}

func (h *handlers) executeClaimedTaskItem(task model.Task, item *model.TaskItem, lockID string) []string {
	return h.executeClaimedTaskItemContext(context.Background(), task, item, lockID)
}
func (h *handlers) executeClaimedTaskItemContext(ctx context.Context, task model.Task, item *model.TaskItem, lockID string) []string {
	return h.services.BatchItemRunner(h).Run(ctx, jobs.BatchItemClaim{TaskID: task.ID, ItemID: item.ID, Token: lockID})
}

func (h *handlers) finishTask(task model.Task, lockID string, errorsSeen []string) error {
	return h.services.BatchLifecycle().Finish(context.Background(), task.ID, lockID, errorsSeen)
}

func (h *handlers) executeTaskItemContext(ctx context.Context, task model.Task, item model.TaskItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch task.Type {
	case taskTypeQuota:
		return h.executeQuotaTaskItem(ctx, task, item)
	case taskTypeEmail:
		return h.executeEmailTaskItemContext(ctx, task, item)
	case taskTypeNodeSystemAction:
		return h.executeNodeSystemActionItem(ctx, task, item)
	case taskTypeNodeDetect, taskTypeNodeReconcile, taskTypeNodeLifecycle, taskTypeProtocolDeploy, taskTypeProtocolActive, taskTypeNodeGroupSync:
		return h.executeOperationTaskItem(ctx, task, item)
	default:
		return errors.New("unsupported task type")
	}
}

func (h *handlers) executeQuotaTaskItem(ctx context.Context, task model.Task, item model.TaskItem) error {
	err := h.services.QuotaExecution().Execute(ctx, entitlements.QuotaExecutionClaim{TaskID: task.ID, ItemID: item.ID, Token: task.LockedBy})
	if err == nil {
	}
	return err
}

func (h *handlers) executeEmailTaskItemContext(ctx context.Context, task model.Task, item model.TaskItem) error {
	return h.services.EmailExecution(h.credentialCipher).Execute(ctx, messaging.EmailExecutionClaim{TaskID: task.ID, ItemID: item.ID, Token: task.LockedBy})
}

func (h *handlers) loadSMTPSettings(db *gorm.DB, requireEnabled bool) (smtpSettings, error) {
	return platformstore.LoadSMTPSettings(db, h.credentialCipher, requireEnabled)
}
func validateSMTPDeliverySettings(settings smtpSettings, requireEnabled bool) error {
	return platform.ValidateSMTPDeliverySettings(settings, requireEnabled)
}

func verifySMTPConnection(ctx context.Context, settings smtpSettings) error {
	return application.SMTPDelivery(settings).Check(ctx)
}
func sendSMTPMail(ctx context.Context, settings smtpSettings, recipient, subject, body, messageID string) error {
	return application.SMTPDelivery(settings).Send(ctx, messaging.Message{Recipient: recipient, Subject: subject, Body: body, ID: messageID})
}
func buildSMTPMessage(from, recipient, subject, body, messageID string) string {
	return smtpadapter.BuildMessage(from, recipient, subject, body, messageID)
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
