package platform

import "context"

type SiteCustomizationDefault struct {
	ConfigKey   string
	LegacyKey   string
	Name        string
	Value       string
	ValueType   string
	Description string
	IsPublic    bool
	IsSecret    bool
	Revision    uint64
}

type SiteCustomizationDefaultsRepository interface {
	ReconcileSiteCustomizationDefaults(context.Context, []SiteCustomizationDefault) error
}

type SiteCustomizationDefaults struct {
	Repository SiteCustomizationDefaultsRepository
}

func (s SiteCustomizationDefaults) Reconcile(ctx context.Context, definitions []SiteCustomizationDefault) error {
	return s.Repository.ReconcileSiteCustomizationDefaults(ctx, definitions)
}
