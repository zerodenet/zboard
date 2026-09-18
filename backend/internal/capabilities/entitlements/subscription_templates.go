package entitlements

import (
	"context"
	"errors"
	"time"
)

var (
	ErrTemplateNotFound = errors.New("subscription template not found")
	ErrTemplateConflict = errors.New("subscription template revision conflict")
	ErrTemplateRuleSet  = errors.New("subscription template rule set unavailable")
)

type SubscriptionTemplate struct {
	ID            uint
	Name          string
	Slug          string
	Description   string
	Renderer      string
	Customization []byte
	IsActive      bool
	SortOrder     int
	Revision      uint64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SubscriptionTemplateBinding struct {
	RuleSetID uint
	Action    string
	Position  int
}

type SubscriptionTemplateDefault struct {
	ID            uint
	Renderer      string
	Customization []byte
}

type SubscriptionTemplateQuery struct {
	Active *bool
	Search string
	Paged  bool
	Offset int
	Limit  int
}

type SubscriptionTemplatePage struct {
	Items []SubscriptionTemplate
	Total int64
}

type SubscriptionTemplateRenderSource struct {
	Template SubscriptionTemplate
	SiteName string
}

type SubscriptionTemplateRepository interface {
	ReconcileTemplateDefaults(context.Context, []SubscriptionTemplateDefault) error
	SaveTemplate(context.Context, uint, SubscriptionTemplate, *uint64, []SubscriptionTemplateBinding) (SubscriptionTemplate, uint64, error)
	DeleteTemplate(context.Context, uint, uint) error
	EnsureClientTemplates(context.Context, []SubscriptionTemplate) error
	SeedClientTemplates(context.Context, []SubscriptionTemplate, string, string) error
	ListTemplates(context.Context, SubscriptionTemplateQuery) (SubscriptionTemplatePage, error)
	TemplateByID(context.Context, uint) (SubscriptionTemplate, error)
	ActiveTemplateBySlug(context.Context, string) (SubscriptionTemplateRenderSource, error)
}

type SubscriptionTemplates struct {
	Repository SubscriptionTemplateRepository
}

func (s SubscriptionTemplates) ReconcileDefaults(ctx context.Context, updates []SubscriptionTemplateDefault) error {
	return s.Repository.ReconcileTemplateDefaults(ctx, updates)
}

func (s SubscriptionTemplates) Save(ctx context.Context, actor uint, template SubscriptionTemplate, expected *uint64, bindings []SubscriptionTemplateBinding) (SubscriptionTemplate, uint64, error) {
	if actor == 0 {
		return SubscriptionTemplate{}, 0, ErrAdministrativeRead
	}
	return s.Repository.SaveTemplate(ctx, actor, template, expected, bindings)
}

func (s SubscriptionTemplates) Delete(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrAdministrativeRead
	}
	return s.Repository.DeleteTemplate(ctx, actor, id)
}

func (s SubscriptionTemplates) EnsureClients(ctx context.Context, definitions []SubscriptionTemplate) error {
	return s.Repository.EnsureClientTemplates(ctx, definitions)
}

func (s SubscriptionTemplates) SeedClients(ctx context.Context, definitions []SubscriptionTemplate, action, detail string) error {
	return s.Repository.SeedClientTemplates(ctx, definitions, action, detail)
}

func (s SubscriptionTemplates) List(ctx context.Context, query SubscriptionTemplateQuery) (SubscriptionTemplatePage, error) {
	return s.Repository.ListTemplates(ctx, query)
}

func (s SubscriptionTemplates) Get(ctx context.Context, id uint) (SubscriptionTemplate, error) {
	if id == 0 {
		return SubscriptionTemplate{}, ErrTemplateNotFound
	}
	return s.Repository.TemplateByID(ctx, id)
}

func (s SubscriptionTemplates) RenderSource(ctx context.Context, slug string) (SubscriptionTemplateRenderSource, error) {
	if slug == "" {
		return SubscriptionTemplateRenderSource{}, ErrTemplateNotFound
	}
	return s.Repository.ActiveTemplateBySlug(ctx, slug)
}
