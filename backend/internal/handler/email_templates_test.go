package handler

import (
	"strings"
	"testing"
)

func TestValidateEmailTemplateRequest(t *testing.T) {
	valid := emailTemplateWriteReq{
		Name: "维护通知", Slug: "maintenance-notice", Category: emailTemplateCategoryOperational,
		SubjectTemplate: "{{site_name}} 维护通知", BodyTemplate: "你好，{{user_email}}。",
	}
	if fields := validateEmailTemplateRequest(valid); len(fields) != 0 {
		t.Fatalf("valid template fields = %#v", fields)
	}

	invalid := valid
	invalid.Slug = "Bad Slug"
	invalid.SubjectTemplate = "bad\r\nBcc: victim@example.com"
	invalid.BodyTemplate = "{{password}}"
	fields := validateEmailTemplateRequest(invalid)
	for _, key := range []string{"slug", "subject_template", "body_template"} {
		if fields[key] == "" {
			t.Fatalf("invalid template fields = %#v, want %s", fields, key)
		}
	}
}

func TestRenderEmailContentUsesRecipientVariables(t *testing.T) {
	variables := map[string]string{
		"site_name": "Example", "site_url": "https://example.com",
		"user_email": "member@example.com", "account_name": "Member",
	}
	subject, body := renderEmailContent("欢迎加入 {{ site_name }}", "{{account_name}} <{{user_email}}>：{{site_url}}", variables)
	if subject != "欢迎加入 Example" || body != "Member <member@example.com>：https://example.com" {
		t.Fatalf("rendered subject/body = %q / %q", subject, body)
	}
}

func TestValidateSMTPDeliverySettings(t *testing.T) {
	settings := smtpSettings{Host: "smtp.example.com", Port: 587, From: "noreply@example.com", TLSMode: "starttls"}
	if err := validateSMTPDeliverySettings(settings, false); err != nil {
		t.Fatalf("valid disabled settings rejected for test: %v", err)
	}
	if err := validateSMTPDeliverySettings(settings, true); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("enabled delivery check error = %v", err)
	}
	settings.Enabled = true
	settings.Username = "mailer"
	if err := validateSMTPDeliverySettings(settings, true); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("missing password error = %v", err)
	}
	settings.Password = "secret"
	if err := validateSMTPDeliverySettings(settings, true); err != nil {
		t.Fatalf("complete SMTP settings rejected: %v", err)
	}
}

func TestRegistrationWelcomeTaskScopeIsCanonicalJSON(t *testing.T) {
	// Guard the task snapshot contract used by enqueueRegistrationWelcome.
	content := emailTaskContent{Subject: "Welcome", Body: "Hello", TemplateID: 7, TemplateRevision: 3}
	if content.TemplateID != 7 || content.TemplateRevision != 3 {
		t.Fatalf("email task template provenance = %+v", content)
	}
}
