package observability

import "context"

type EntityReference struct {
	ID          uint   `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	Secondary   string `json:"secondary,omitempty"`
	Status      string `json:"status,omitempty"`
	Missing     bool   `json:"missing,omitempty"`
}

type EntityReferenceRequest struct {
	Users, Subscriptions, Nodes, ProtocolEndpoints, Plans, PlanSKUs, Orders []uint
}

type EntityReferenceData struct {
	Users             map[string]EntityReference `json:"users"`
	Subscriptions     map[string]EntityReference `json:"subscriptions"`
	Nodes             map[string]EntityReference `json:"nodes"`
	ProtocolEndpoints map[string]EntityReference `json:"protocol_endpoints"`
	Plans             map[string]EntityReference `json:"plans"`
	PlanSKUs          map[string]EntityReference `json:"plan_skus"`
	Orders            map[string]EntityReference `json:"orders"`
}

type EntityReferenceRepository interface {
	Resolve(context.Context, EntityReferenceRequest, EntityReferenceData) error
}

type EntityReferences struct{ Repository EntityReferenceRepository }

func (s EntityReferences) Resolve(ctx context.Context, request EntityReferenceRequest, data EntityReferenceData) error {
	return s.Repository.Resolve(ctx, request, data)
}
