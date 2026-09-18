package messaging

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrTemplatePermission = errors.New("templates require current administrator")
	ErrTemplateNotFound   = errors.New("email template not found")
	ErrTemplateSlug       = errors.New("email template slug already exists")
	ErrTemplateProtected  = errors.New("registration templates cannot be deleted; disable the template instead")
)

type TemplateConflict struct{ CurrentRevision uint64 }

func (e *TemplateConflict) Error() string { return "email template revision conflict" }

type TemplateValidation struct{ Fields map[string]string }

func (e *TemplateValidation) Error() string { return "邮件模板信息校验失败。" }

type TemplateRepository interface {
	List(context.Context, uint, string) ([]Template, error)
	Save(context.Context, uint, uint, TemplateWrite) (Template, error)
	Delete(context.Context, uint, uint) error
	PreviewVariables(context.Context, uint) (map[string]string, error)
}
type Templates struct{ Repository TemplateRepository }

func (s Templates) List(ctx context.Context, actor uint, category string) ([]Template, error) {
	if actor == 0 {
		return nil, ErrTemplatePermission
	}
	category = strings.TrimSpace(category)
	if category != "" && category != TemplateRegistration && category != TemplateOperational {
		return nil, &TemplateValidation{Fields: map[string]string{"category": "category must be registration or operational"}}
	}
	return s.Repository.List(ctx, actor, category)
}
func (s Templates) Save(ctx context.Context, actor, id uint, in TemplateWrite) (Template, error) {
	if actor == 0 {
		return Template{}, ErrTemplatePermission
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	in.Category = strings.ToLower(strings.TrimSpace(in.Category))
	in.SubjectTemplate = strings.TrimSpace(in.SubjectTemplate)
	fields := ValidateTemplateRequest(in)
	if id == 0 && in.Category != TemplateOperational {
		fields["category"] = "只能新增运营模板；注册通知由系统提供并允许编辑。"
	}
	if len(fields) > 0 {
		return Template{}, &TemplateValidation{Fields: fields}
	}
	return s.Repository.Save(ctx, actor, id, in)
}
func (s Templates) Delete(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrTemplatePermission
	}
	if id == 0 {
		return ErrTemplateNotFound
	}
	return s.Repository.Delete(ctx, actor, id)
}
func (s Templates) Preview(ctx context.Context, actor uint, in TemplatePreviewInput) (TemplatePreview, error) {
	if actor == 0 {
		return TemplatePreview{}, ErrTemplatePermission
	}
	in.Category = strings.ToLower(strings.TrimSpace(in.Category))
	in.SubjectTemplate = strings.TrimSpace(in.SubjectTemplate)
	fields := ValidateTemplateContent(in.SubjectTemplate, in.BodyTemplate)
	if in.Category != TemplateRegistration && in.Category != TemplateOperational {
		fields["category"] = "请选择注册通知或运营模板。"
	}
	if len(fields) > 0 {
		return TemplatePreview{}, &TemplateValidation{Fields: fields}
	}
	variables, err := s.Repository.PreviewVariables(ctx, actor)
	if err != nil {
		return TemplatePreview{}, err
	}
	subject, body := RenderContent(in.SubjectTemplate, in.BodyTemplate, variables)
	return TemplatePreview{Subject: subject, Body: body, Variables: variables}, nil
}
