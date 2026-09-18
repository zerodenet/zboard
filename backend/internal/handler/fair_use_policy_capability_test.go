package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestFairUsePolicyCapabilityRevisionPermissionAndRollback(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	actor, err := h.authFromRequest(announcementRequest("GET", "/", token, ""))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	scope := metering.PolicyScope{Type: "platform"}
	raw, _ := json.Marshal(metering.DefaultPolicy("platform", 0))
	var input metering.PolicyInput
	_ = json.Unmarshal(raw, &input)
	if _, err = h.services.FairUsePolicies.Save(ctx, actor.UserID, scope, input); !errors.Is(err, metering.ErrPolicyPermission) {
		t.Fatalf("nonadmin: %v", err)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	input.Enabled = true
	out, err := h.services.FairUsePolicies.Save(ctx, actor.UserID, scope, input)
	if err != nil || out.Effective.Revision != 1 {
		t.Fatalf("create: %+v %v", out, err)
	}
	if _, err = h.services.FairUsePolicies.Save(ctx, actor.UserID, scope, input); !errors.Is(err, metering.ErrPolicyRevisionConflict) {
		t.Fatalf("stale write: %v", err)
	}
	input.ExpectedRevision = 1
	input.Enabled = false
	out, err = h.services.FairUsePolicies.Save(ctx, actor.UserID, scope, input)
	if err != nil || out.Effective.Enabled || out.Effective.Revision != 2 {
		t.Fatalf("disable: %+v %v", out, err)
	}
	if err = h.db.Exec("CREATE TRIGGER reject_policy_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'fair_use.policy.save' BEGIN SELECT RAISE(ABORT, 'audit rejected'); END").Error; err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision = 2
	input.Enabled = true
	if _, err = h.services.FairUsePolicies.Save(ctx, actor.UserID, scope, input); err == nil {
		t.Fatal("expected audit failure")
	}
	out, err = h.services.FairUsePolicies.Read(ctx, actor.UserID, scope)
	if err != nil || out.Effective.Revision != 2 || out.Effective.Enabled {
		t.Fatalf("rollback: %+v %v", out, err)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = h.services.FairUsePolicies.Read(ctx, actor.UserID, scope); !errors.Is(err, metering.ErrPolicyPermission) {
		t.Fatalf("revoked admin: %v", err)
	}
}
