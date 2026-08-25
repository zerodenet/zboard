package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	emailTemplateCategoryRegistration = "registration"
	emailTemplateCategoryOperational  = "operational"
	emailTriggerUserRegistered        = "user.registered"
)

var (
	emailTemplateSlugPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	emailTemplateVariablePattern  = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)
	allowedEmailTemplateVariables = map[string]struct{}{
		"site_name": {}, "site_url": {}, "user_email": {}, "account_name": {},
		"registered_at": {}, "current_date": {},
	}
	errEmailTemplateRevisionConflict = errors.New("email template revision conflict")
)

type emailTemplateWriteReq struct {
	Name             string  `json:"name"`
	Slug             string  `json:"slug"`
	Category         string  `json:"category"`
	SubjectTemplate  string  `json:"subject_template"`
	BodyTemplate     string  `json:"body_template"`
	IsActive         *bool   `json:"is_active"`
	SortOrder        int     `json:"sort_order"`
	ExpectedRevision *uint64 `json:"expected_revision"`
}

type emailTemplatePreviewReq struct {
	Category        string `json:"category"`
	SubjectTemplate string `json:"subject_template"`
	BodyTemplate    string `json:"body_template"`
}

type emailTemplatePreview struct {
	Subject   string            `json:"subject"`
	Body      string            `json:"body"`
	Variables map[string]string `json:"variables"`
}

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
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	query := h.db.Model(&model.EmailTemplate{})
	if category := strings.TrimSpace(r.URL.Query().Get("category")); category != "" {
		if category != emailTemplateCategoryRegistration && category != emailTemplateCategoryOperational {
			BadRequest(w, "category must be registration or operational")
			return
		}
		query = query.Where("category = ?", category)
	}
	items := make([]model.EmailTemplate, 0)
	if err := query.Order("category asc, sort_order asc, id asc").Find(&items).Error; err != nil {
		ServerError(w, err)
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
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Category = strings.ToLower(strings.TrimSpace(req.Category))
	req.SubjectTemplate = strings.TrimSpace(req.SubjectTemplate)
	fields := validateEmailTemplateRequest(req)
	if id == 0 && req.Category != emailTemplateCategoryOperational {
		fields["category"] = "只能新增运营模板；注册通知由系统提供并允许编辑。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "邮件模板信息校验失败。", fields)
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	var saved model.EmailTemplate
	var currentRevision uint64
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if id == 0 {
			saved = model.EmailTemplate{
				Name: req.Name, Slug: req.Slug, Category: emailTemplateCategoryOperational,
				SubjectTemplate: req.SubjectTemplate, BodyTemplate: req.BodyTemplate,
				IsActive: active, SortOrder: req.SortOrder, Revision: 1,
			}
			if err := tx.Create(&saved).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&saved, id).Error; err != nil {
				return err
			}
			currentRevision = saved.Revision
			if req.ExpectedRevision != nil && saved.Revision != *req.ExpectedRevision {
				return errEmailTemplateRevisionConflict
			}
			if saved.Category == emailTemplateCategoryRegistration {
				req.Category = saved.Category
				req.Slug = saved.Slug
			} else if req.Category != emailTemplateCategoryOperational {
				return validationError("邮件模板信息校验失败。", map[string]string{"category": "运营模板不能转换为注册通知模板。"})
			}
			saved.Name = req.Name
			saved.Slug = req.Slug
			saved.SubjectTemplate = req.SubjectTemplate
			saved.BodyTemplate = req.BodyTemplate
			saved.IsActive = active
			saved.SortOrder = req.SortOrder
			saved.Revision++
			if err := tx.Save(&saved).Error; err != nil {
				return err
			}
		}
		action := "email_template.create"
		if id != 0 {
			action = "email_template.update"
		}
		return createAuditLog(tx, claims, action, fmt.Sprintf("email_template:%d", saved.ID), fmt.Sprintf("category=%s slug=%s revision=%d", saved.Category, saved.Slug, saved.Revision))
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, errEmailTemplateRevisionConflict) {
		writeJSON(w, http.StatusConflict, "邮件模板已被其他管理员更新，请重新载入。", map[string]interface{}{"current_revision": currentRevision})
		return
	}
	var validation *requestValidationError
	if errors.As(err, &validation) {
		BadRequestError(w, validation)
		return
	}
	if err != nil {
		if isDuplicateError(err) {
			BadRequestFields(w, "邮件模板信息校验失败。", map[string]string{"slug": "模板标识已存在，请更换后重试。"})
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, saved)
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
	var item model.EmailTemplate
	if err := h.db.First(&item, id).Error; err != nil {
		NotFound(w)
		return
	}
	if item.Category == emailTemplateCategoryRegistration {
		BadRequest(w, "registration templates cannot be deleted; disable the template instead")
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&item).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "email_template.delete", fmt.Sprintf("email_template:%d", item.ID), "slug="+item.Slug)
	}); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true})
}

func (h *handlers) AdminEmailTemplatePreviewHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var req emailTemplatePreviewReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Category = strings.ToLower(strings.TrimSpace(req.Category))
	req.SubjectTemplate = strings.TrimSpace(req.SubjectTemplate)
	fields := validateEmailTemplateContent(req.SubjectTemplate, req.BodyTemplate)
	if req.Category != emailTemplateCategoryRegistration && req.Category != emailTemplateCategoryOperational {
		fields["category"] = "请选择注册通知或运营模板。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "邮件模板预览校验失败。", fields)
		return
	}
	variables := h.sampleEmailTemplateVariables()
	subject, body := renderEmailContent(req.SubjectTemplate, req.BodyTemplate, variables)
	OK(w, emailTemplatePreview{Subject: subject, Body: body, Variables: variables})
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
	settings, err := h.loadSMTPSettings(h.db, false)
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
		variables := h.sampleEmailTemplateVariables()
		subject := "[" + variables["site_name"] + "] SMTP 测试邮件"
		body := "这是一封由 Zboard 管理端发起的 SMTP 投递测试邮件。\n\n若你收到此邮件，说明连接、TLS、认证、发件人与收件人投递链路均已通过。"
		err = sendSMTPMail(ctx, settings, recipient, subject, body, fmt.Sprintf("<smtp-test-%s@%s>", uuid.NewString(), settings.Host))
	} else {
		err = verifySMTPConnection(ctx, settings)
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	duration := time.Since(started).Milliseconds()
	if err := createAuditLog(h.db, claims, "smtp.test", "system_config:smtp", fmt.Sprintf("mode=%s duration_ms=%d", req.Mode, duration)); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, smtpTestResult{
		Mode: req.Mode, TLSMode: settings.TLSMode, Authenticated: settings.Username != "",
		Recipient: recipient, DurationMS: duration,
	})
}

func validateEmailTemplateRequest(req emailTemplateWriteReq) map[string]string {
	fields := validateEmailTemplateContent(req.SubjectTemplate, req.BodyTemplate)
	if req.Name == "" || len(req.Name) > 80 {
		fields["name"] = "模板名称需包含 1 到 80 个 UTF-8 字节。"
	}
	if len(req.Slug) > 80 || !emailTemplateSlugPattern.MatchString(req.Slug) {
		fields["slug"] = "模板标识只能包含小写字母、数字和单个连字符，且不能超过 80 个字符。"
	}
	if req.Category != emailTemplateCategoryRegistration && req.Category != emailTemplateCategoryOperational {
		fields["category"] = "请选择注册通知或运营模板。"
	}
	return fields
}

func validateEmailTemplateContent(subject, body string) map[string]string {
	fields := map[string]string{}
	subject = strings.TrimSpace(subject)
	if subject == "" || len(subject) > 200 || strings.ContainsAny(subject, "\r\n") {
		fields["subject_template"] = "邮件主题需包含 1 到 200 个 UTF-8 字节，且不能换行。"
	}
	if strings.TrimSpace(body) == "" || len(body) > 100000 {
		fields["body_template"] = "邮件正文需包含 1 到 100000 个 UTF-8 字节。"
	}
	unknown := make(map[string]struct{})
	for _, content := range []string{subject, body} {
		for _, match := range emailTemplateVariablePattern.FindAllStringSubmatch(content, -1) {
			if _, ok := allowedEmailTemplateVariables[match[1]]; !ok {
				unknown[match[1]] = struct{}{}
			}
		}
	}
	if len(unknown) > 0 {
		values := make([]string, 0, len(unknown))
		for variable := range unknown {
			values = append(values, "{{"+variable+"}}")
		}
		sort.Strings(values)
		fields["body_template"] = "包含不支持的变量：" + strings.Join(values, "、")
	}
	return fields
}

func renderEmailContent(subject, body string, variables map[string]string) (string, string) {
	render := func(content string) string {
		return emailTemplateVariablePattern.ReplaceAllStringFunc(content, func(token string) string {
			parts := emailTemplateVariablePattern.FindStringSubmatch(token)
			if len(parts) != 2 {
				return token
			}
			if value, ok := variables[parts[1]]; ok {
				return value
			}
			return token
		})
	}
	return render(subject), render(body)
}

func (h *handlers) sampleEmailTemplateVariables() map[string]string {
	variables := map[string]string{
		"site_name": "Zboard", "site_url": "https://panel.example.com",
		"user_email": "member@example.com", "account_name": "member@example.com",
		"registered_at": "2026-08-24 12:00 UTC", "current_date": "2026-08-24",
	}
	if siteName, siteURL, err := h.loadEmailSiteIdentity(); err == nil {
		variables["site_name"] = siteName
		variables["site_url"] = siteURL
	}
	return variables
}

func (h *handlers) loadEmailSiteIdentity() (string, string, error) {
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err != nil {
		return "", "", err
	}
	siteName := strings.TrimSpace(installation.SiteName)
	if siteName == "" {
		siteName = "Zboard"
	}
	return siteName, strings.TrimSpace(installation.SiteURL), nil
}

func emailVariablesForUser(user model.User, siteName, siteURL string) map[string]string {
	if strings.TrimSpace(siteName) == "" {
		siteName = "Zboard"
	}
	variables := map[string]string{
		"site_name": siteName, "site_url": strings.TrimSpace(siteURL),
	}
	variables["user_email"] = user.Email
	variables["account_name"] = strings.TrimSpace(user.AccountName)
	if variables["account_name"] == "" {
		variables["account_name"] = user.Email
	}
	registeredAt := user.CreatedAt.UTC()
	if registeredAt.IsZero() {
		registeredAt = time.Now().UTC()
	}
	variables["registered_at"] = registeredAt.Format("2006-01-02 15:04 UTC")
	variables["current_date"] = time.Now().UTC().Format("2006-01-02")
	return variables
}

func (h *handlers) snapshotEmailTaskContent(raw json.RawMessage) (json.RawMessage, error) {
	var content emailTaskContent
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, err
	}
	siteName, siteURL, err := h.loadEmailSiteIdentity()
	if err != nil {
		return nil, err
	}
	content.SiteName = siteName
	content.SiteURL = siteURL
	encoded, err := json.Marshal(content)
	return json.RawMessage(encoded), err
}

func (h *handlers) enqueueRegistrationWelcome(user model.User) error {
	if _, err := h.loadSMTPSettings(h.db, true); err != nil {
		return nil
	}
	var template model.EmailTemplate
	if err := h.db.Where("category = ? AND trigger_key = ? AND is_active = ?", emailTemplateCategoryRegistration, emailTriggerUserRegistered, true).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	siteName, siteURL, err := h.loadEmailSiteIdentity()
	if err != nil {
		return err
	}
	content, err := json.Marshal(emailTaskContent{
		Subject: template.SubjectTemplate, Body: template.BodyTemplate,
		TemplateID: template.ID, TemplateRevision: template.Revision,
		SiteName: siteName, SiteURL: siteURL,
	})
	if err != nil {
		return err
	}
	task := model.Task{
		Type: taskTypeEmail, Scope: fmt.Sprintf(`{"user_ids":[%d]}`, user.ID), Content: string(content),
		Status: taskStatusPending, Total: 1, IdempotencyKey: fmt.Sprintf("registration-welcome:%d:%d", user.ID, template.Revision),
		MaxAttempts: 3,
	}
	item := newTaskItem("user", user.ID)
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		item.TaskID = task.ID
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{Actor: "system", Action: "notification.enqueue", Target: fmt.Sprintf("task:%d", task.ID), Detail: fmt.Sprintf("trigger=%s template_id=%d revision=%d", emailTriggerUserRegistered, template.ID, template.Revision)}).Error
	})
	if err != nil {
		if isDuplicateError(err) {
			return nil
		}
		return err
	}
	return h.startPersistedAdminTask(&task)
}
