package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	emailTemplateCategoryRegistration = "registration"
	emailTemplateCategoryOperational  = "operational"
	emailTriggerUserRegistered        = "user.registered"
)

type emailTemplateWriteReq = messaging.TemplateWrite
type emailTemplatePreviewReq = messaging.TemplatePreviewInput
type emailTemplatePreview = messaging.TemplatePreview

type smtpTestReq struct {
	Mode      string `json:"mode"`
	Recipient string `json:"recipient"`
}

type smtpTestResult struct {
	Mode          string `json:"mode"`
	TLSMode       string `json:"tls_mode"`
	Authenticated bool   `json:"authenticated"`
	Recipient     string `json:"recipient,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
}

func (h *handlers) AdminEmailTemplatesListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	items, err := h.services.MessageTemplates.List(r.Context(), claims.UserID, r.URL.Query().Get("category"))
	if templateResponseError(w, err) {
		return
	}
	OK(w, items)
}
func (h *handlers) AdminEmailTemplateCreateHandler(w http.ResponseWriter, r *http.Request) {
	h.saveEmailTemplate(w, r, 0)
}
func (h *handlers) AdminEmailTemplateUpdateHandler(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/email-templates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.saveEmailTemplate(w, r, id)
}
func (h *handlers) saveEmailTemplate(w http.ResponseWriter, r *http.Request, id uint) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req emailTemplateWriteReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.MessageTemplates.Save(r.Context(), claims.UserID, id, req)
	if templateResponseError(w, err) {
		return
	}
	OK(w, result)
}
func (h *handlers) AdminEmailTemplateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/email-templates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if templateResponseError(w, h.services.MessageTemplates.Delete(r.Context(), claims.UserID, id)) {
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true})
}
func (h *handlers) AdminEmailTemplatePreviewHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req emailTemplatePreviewReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.MessageTemplates.Preview(r.Context(), claims.UserID, req)
	if templateResponseError(w, err) {
		return
	}
	OK(w, result)
}
func templateResponseError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var conflict *messaging.TemplateConflict
	var invalid *messaging.TemplateValidation
	switch {
	case errors.Is(err, messaging.ErrTemplatePermission):
		Forbidden(w, "templates require current administrator")
	case errors.Is(err, messaging.ErrTemplateNotFound):
		NotFound(w)
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, "邮件模板已被其他管理员更新，请重新载入。", map[string]interface{}{"current_revision": conflict.CurrentRevision})
	case errors.As(err, &invalid):
		BadRequestFields(w, invalid.Error(), invalid.Fields)
	case errors.Is(err, messaging.ErrTemplateSlug):
		BadRequestFields(w, "邮件模板信息校验失败。", map[string]string{"slug": "模板标识已存在，请更换后重试。"})
	case errors.Is(err, messaging.ErrTemplateProtected):
		BadRequest(w, err.Error())
	default:
		writeJSON(w, http.StatusInternalServerError, "email template operation failed", nil)
	}
	return true
}

func (h *handlers) AdminSMTPTestHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req smtpTestReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	req.Recipient = normalizeEmail(req.Recipient)
	if req.Mode == "" {
		req.Mode = "connection"
	}
	if req.Mode != "connection" && req.Mode != "delivery" {
		BadRequestFields(w, "SMTP 测试参数校验失败。", map[string]string{"mode": "测试模式必须为 connection 或 delivery。"})
		return
	}
	if req.Mode == "delivery" && req.Recipient != "" && !validEmail(req.Recipient) {
		BadRequestFields(w, "SMTP 测试参数校验失败。", map[string]string{"recipient": "请输入有效的测试收件邮箱。"})
		return
	}
	settings, err := h.services.SMTPSettings(r.Context(), h.credentialCipher, false)
	if err == nil {
		err = validateSMTPDeliverySettings(settings, false)
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	started := time.Now()
	recipient := ""
	if req.Mode == "delivery" {
		recipient = req.Recipient
		if recipient == "" {
			recipient = claims.Email
		}
		body := "这是一封由 Zboard 管理端发起的 SMTP 投递测试邮件。\n\n若你收到此邮件，说明连接、TLS、认证、发件人与收件人投递链路均已通过。"
		preview, previewErr := h.services.MessageTemplates.Preview(ctx, claims.UserID, messaging.TemplatePreviewInput{Category: messaging.TemplateOperational, SubjectTemplate: "[{{site_name}}] SMTP 测试邮件", BodyTemplate: body})
		if templateResponseError(w, previewErr) {
			return
		}
		err = sendSMTPMail(ctx, settings, recipient, preview.Subject, preview.Body, fmt.Sprintf("<smtp-test-%s@%s>", uuid.NewString(), settings.Host))
	} else {
		err = verifySMTPConnection(ctx, settings)
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	duration := time.Since(started).Milliseconds()
	if err := h.services.Audit.RecordAdmin(ctx, claims.UserID, observability.AuditEvent{
		Action: "smtp.test", Target: "system_config:smtp", Detail: fmt.Sprintf("mode=%s duration_ms=%d", req.Mode, duration),
	}); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, smtpTestResult{
		Mode: req.Mode, TLSMode: settings.TLSMode, Authenticated: settings.Username != "",
		Recipient: recipient, DurationMS: duration,
	})
}

func validateEmailTemplateRequest(req emailTemplateWriteReq) map[string]string {
	return messaging.ValidateTemplateRequest(req)
}
func validateEmailTemplateContent(subject, body string) map[string]string {
	return messaging.ValidateTemplateContent(subject, body)
}
func renderEmailContent(subject, body string, variables map[string]string) (string, string) {
	return messaging.RenderContent(subject, body, variables)
}

func (h *handlers) loadEmailSiteIdentity(ctx context.Context) (string, string, error) {
	installation, err := h.services.Installation.Status(ctx)
	if err != nil {
		return "", "", err
	}
	siteName := strings.TrimSpace(installation.SiteName)
	if siteName == "" {
		siteName = "Zboard"
	}
	return siteName, strings.TrimSpace(installation.SiteURL), nil
}
