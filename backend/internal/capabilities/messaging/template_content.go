package messaging

import (
	"regexp"
	"sort"
	"strings"
)

const (
	TemplateRegistration = "registration"
	TemplateOperational  = "operational"
)

var (
	emailTemplateSlugPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	emailTemplateVariablePattern  = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)
	allowedEmailTemplateVariables = map[string]struct{}{
		"site_name": {}, "site_url": {}, "user_email": {}, "account_name": {},
		"registered_at": {}, "current_date": {},
	}
)

type TemplateWrite struct {
	Name             string  `json:"name"`
	Slug             string  `json:"slug"`
	Category         string  `json:"category"`
	SubjectTemplate  string  `json:"subject_template"`
	BodyTemplate     string  `json:"body_template"`
	IsActive         *bool   `json:"is_active"`
	SortOrder        int     `json:"sort_order"`
	ExpectedRevision *uint64 `json:"expected_revision"`
}

type TemplatePreviewInput struct {
	Category        string `json:"category"`
	SubjectTemplate string `json:"subject_template"`
	BodyTemplate    string `json:"body_template"`
}

type TemplatePreview struct {
	Subject   string            `json:"subject"`
	Body      string            `json:"body"`
	Variables map[string]string `json:"variables"`
}

func ValidateTemplateRequest(req TemplateWrite) map[string]string {
	fields := ValidateTemplateContent(req.SubjectTemplate, req.BodyTemplate)
	if req.Name == "" || len(req.Name) > 80 {
		fields["name"] = "模板名称需包含 1 到 80 个 UTF-8 字节。"
	}
	if len(req.Slug) > 80 || !emailTemplateSlugPattern.MatchString(req.Slug) {
		fields["slug"] = "模板标识只能包含小写字母、数字和单个连字符，且不能超过 80 个字符。"
	}
	if req.Category != TemplateRegistration && req.Category != TemplateOperational {
		fields["category"] = "请选择注册通知或运营模板。"
	}
	return fields
}

func ValidateTemplateContent(subject, body string) map[string]string {
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

func RenderContent(subject, body string, variables map[string]string) (string, string) {
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
