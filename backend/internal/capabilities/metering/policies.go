package metering

import (
	"context"
	"errors"
)

var ErrPolicyPermission = errors.New("current administrator required")
var ErrPolicyNotFound = errors.New("policy resource not found")
var ErrPolicyRevisionConflict = errors.New("Fair Use policy revision conflict")

type PolicyScope struct {
	Type string
	ID   uint
}
type PolicyRepository interface {
	Read(context.Context, uint, PolicyScope) (PolicyResolution, error)
	Save(context.Context, uint, PolicyScope, PolicyInput) (PolicyResolution, error)
	Delete(context.Context, uint, PolicyScope) (PolicyResolution, error)
}
type Policies struct{ Repository PolicyRepository }

func validateScope(actor uint, scope PolicyScope) error {
	if actor == 0 {
		return ErrPolicyPermission
	}
	if (scope.Type == "platform" && scope.ID == 0) || ((scope.Type == "plan" || scope.Type == "subscription") && scope.ID != 0) {
		return nil
	}
	return &PolicyValidation{Fields: map[string]string{"scope": "invalid policy scope"}}
}
func (s Policies) Read(ctx context.Context, actor uint, scope PolicyScope) (PolicyResolution, error) {
	if err := validateScope(actor, scope); err != nil {
		return PolicyResolution{}, err
	}
	return s.Repository.Read(ctx, actor, scope)
}
func (s Policies) Save(ctx context.Context, actor uint, scope PolicyScope, input PolicyInput) (PolicyResolution, error) {
	if err := validateScope(actor, scope); err != nil {
		return PolicyResolution{}, err
	}
	if err := ValidatePolicy(input); err != nil {
		return PolicyResolution{}, err
	}
	return s.Repository.Save(ctx, actor, scope, input)
}
func (s Policies) Delete(ctx context.Context, actor uint, scope PolicyScope) (PolicyResolution, error) {
	if err := validateScope(actor, scope); err != nil {
		return PolicyResolution{}, err
	}
	if scope.Type == "platform" {
		return PolicyResolution{}, &PolicyValidation{Fields: map[string]string{"scope": "platform policy cannot be deleted"}}
	}
	return s.Repository.Delete(ctx, actor, scope)
}
