package commerce

import "time"

type LegacyPlanGroup struct {
	ID                  uint      `json:"id"`
	Name                string    `json:"name"`
	Code                string    `json:"code"`
	Description         string    `json:"description"`
	IsEnabled           bool      `json:"is_enabled"`
	Revision            uint64    `json:"revision"`
	ProtocolEndpointIDs []uint    `json:"protocol_endpoint_ids"`
	NetworkEntryIDs     []uint    `json:"network_entry_ids"`
	PlanCount           int64     `json:"plan_count"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type LegacyPlan struct {
	Plan
	NodeGroup *LegacyPlanGroup `json:"node_group,omitempty"`
}
