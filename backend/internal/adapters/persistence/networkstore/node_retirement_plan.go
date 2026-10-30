package networkstore

import (
	"encoding/json"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Corrupt/missing plans must never degrade into an empty cleanup followed by
// archiving management. A detach may intentionally have an empty plan.
func validateRetirementPlan(row model.NodeRetirement) error {
	var steps []network.RetirementStep
	if row.Mode != "retire" && row.Mode != "detach" {
		return jobs.ErrInvalid
	}
	if json.Unmarshal([]byte(row.Steps), &steps) != nil || (row.Mode == "retire" && len(steps) == 0) {
		return jobs.ErrInvalid
	}
	seen := map[string]bool{}
	for _, step := range steps {
		key := fmt.Sprintf("%s:%d", step.Kind, step.ID)
		if seen[key] || step.ID == 0 || (step.Kind != "dns" && step.Kind != "certificate" && step.Kind != "zero") || (step.Kind == "zero" && step.ID != row.NodeID) {
			return jobs.ErrInvalid
		}
		seen[key] = true
		switch step.State {
		case "pending", "running", "succeeded", "retained", "failed":
		default:
			return jobs.ErrInvalid
		}
	}
	if row.Mode == "retire" && !seen[fmt.Sprintf("zero:%d", row.NodeID)] {
		return jobs.ErrInvalid
	}
	return nil
}
