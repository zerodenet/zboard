package entitlements

import (
	"context"
	"errors"
	"time"
)

var (
	ErrRuleSetNotFound            = errors.New("subscription rule set not found")
	ErrRuleSetConflict            = errors.New("subscription rule set revision conflict")
	ErrRuleSetTagImmutable        = errors.New("subscription rule set tag is immutable")
	ErrRuleSetLegacyReadOnly      = errors.New("legacy subscription rule set is read only")
	ErrRuleSetInUse               = errors.New("subscription rule set is in use")
	ErrRuleSetClientCompatibility = errors.New("subscription rule set is incompatible with an existing client")
)

type SubscriptionRuleSet struct {
	ID                   uint
	Name, Description    string
	Renderer, Tag, URL   string
	Behavior, Format     string
	Interval             int
	IsActive             bool
	Revision             uint64
	UsageCount           int64
	CreatedAt, UpdatedAt time.Time
}

type SubscriptionRuleSetQuery struct {
	Keyword       string
	Renderers     []string
	ExcludeFormat string
	Active        *bool
	ID            uint
	IDs           []uint
	Offset, Limit int
}

type SubscriptionRuleSetPage struct {
	Items   []SubscriptionRuleSet
	Total   int64
	SiteURL string
}

type RuleSetContentStore interface {
	Read(context.Context, string) ([]byte, bool, error)
	Write(context.Context, string, []byte) error
	RemoveSource(context.Context, string) error
	RemoveAll(context.Context, string) error
}

type SubscriptionRuleSetRepository interface {
	ListRuleSets(context.Context, uint, SubscriptionRuleSetQuery) (SubscriptionRuleSetPage, error)
	GetRuleSet(context.Context, uint, uint) (SubscriptionRuleSet, string, error)
	GetPublicRuleSet(context.Context, string) (SubscriptionRuleSet, error)
	RuleSetSiteURL(context.Context) (string, error)
	SaveRuleSet(context.Context, uint, SubscriptionRuleSet, *uint64, []byte, bool) (SubscriptionRuleSet, uint64, error)
	ReplaceRuleSetContent(context.Context, uint, SubscriptionRuleSet, *uint64, []byte) (SubscriptionRuleSet, uint64, error)
	DeleteRuleSet(context.Context, uint, uint) (SubscriptionRuleSet, error)
	ResolveRuleSets(context.Context, []uint) ([]SubscriptionRuleSet, string, error)
}

type SubscriptionRuleSets struct{ Repository SubscriptionRuleSetRepository }

func (s SubscriptionRuleSets) List(ctx context.Context, actor uint, query SubscriptionRuleSetQuery) (SubscriptionRuleSetPage, error) {
	if actor == 0 {
		return SubscriptionRuleSetPage{}, ErrAdministrativeRead
	}
	return s.Repository.ListRuleSets(ctx, actor, query)
}

func (s SubscriptionRuleSets) Get(ctx context.Context, actor, id uint) (SubscriptionRuleSet, string, error) {
	if actor == 0 {
		return SubscriptionRuleSet{}, "", ErrAdministrativeRead
	}
	if id == 0 {
		return SubscriptionRuleSet{}, "", ErrRuleSetNotFound
	}
	return s.Repository.GetRuleSet(ctx, actor, id)
}

func (s SubscriptionRuleSets) Public(ctx context.Context, tag string) (SubscriptionRuleSet, error) {
	if tag == "" {
		return SubscriptionRuleSet{}, ErrRuleSetNotFound
	}
	return s.Repository.GetPublicRuleSet(ctx, tag)
}

func (s SubscriptionRuleSets) SiteURL(ctx context.Context) (string, error) {
	return s.Repository.RuleSetSiteURL(ctx)
}

func (s SubscriptionRuleSets) Save(ctx context.Context, actor uint, record SubscriptionRuleSet, expected *uint64, content []byte, replaceContent bool) (SubscriptionRuleSet, uint64, error) {
	if actor == 0 {
		return SubscriptionRuleSet{}, 0, ErrAdministrativeRead
	}
	return s.Repository.SaveRuleSet(ctx, actor, record, expected, content, replaceContent)
}

func (s SubscriptionRuleSets) ReplaceContent(ctx context.Context, actor uint, record SubscriptionRuleSet, expected *uint64, content []byte) (SubscriptionRuleSet, uint64, error) {
	if actor == 0 {
		return SubscriptionRuleSet{}, 0, ErrAdministrativeRead
	}
	return s.Repository.ReplaceRuleSetContent(ctx, actor, record, expected, content)
}

func (s SubscriptionRuleSets) Delete(ctx context.Context, actor, id uint) (SubscriptionRuleSet, error) {
	if actor == 0 {
		return SubscriptionRuleSet{}, ErrAdministrativeRead
	}
	return s.Repository.DeleteRuleSet(ctx, actor, id)
}

func (s SubscriptionRuleSets) Resolve(ctx context.Context, ids []uint) ([]SubscriptionRuleSet, string, error) {
	return s.Repository.ResolveRuleSets(ctx, ids)
}
