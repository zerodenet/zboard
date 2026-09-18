package meteringstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
	"time"
)

type Policies struct{ DB *gorm.DB }

func (s Policies) Read(ctx context.Context, actor uint, scope metering.PolicyScope) (metering.PolicyResolution, error) {
	return s.operate(ctx, actor, scope, "", nil)
}
func (s Policies) Save(ctx context.Context, actor uint, scope metering.PolicyScope, input metering.PolicyInput) (metering.PolicyResolution, error) {
	return s.operate(ctx, actor, scope, "save", &input)
}
func (s Policies) Delete(ctx context.Context, actor uint, scope metering.PolicyScope) (metering.PolicyResolution, error) {
	return s.operate(ctx, actor, scope, "delete", nil)
}
func (s Policies) operate(ctx context.Context, actor uint, scope metering.PolicyScope, action string, input *metering.PolicyInput) (metering.PolicyResolution, error) {
	var out metering.PolicyResolution
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var admin model.User
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "email", "is_admin", "status").First(&admin, actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return metering.ErrPolicyPermission
		}
		if err != nil {
			return err
		}
		if !admin.IsAdmin || admin.Status != "active" {
			return metering.ErrPolicyPermission
		}
		var planID uint
		// Lock the owning resource before changing a scoped override, including its first insert.
		switch scope.Type {
		case "plan":
			var plan model.Plan
			err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&plan, scope.ID).Error
		case "subscription":
			var sub model.Subscription
			err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "plan_id").First(&sub, scope.ID).Error
			planID = sub.PlanID
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return metering.ErrPolicyNotFound
		}
		if err != nil {
			return err
		}
		if action == "save" {
			if err := savePolicy(tx, scope, *input); err != nil {
				return err
			}
		}
		if action == "delete" {
			if err := tx.Where("scope_type = ? AND scope_id = ?", scope.Type, scope.ID).Delete(&PolicyRecord{}).Error; err != nil {
				return err
			}
		}
		if action != "" {
			if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "fair_use.policy." + action, Target: fmt.Sprintf("%s:%d", scope.Type, scope.ID), Detail: "policy override updated"}).Error; err != nil {
				return err
			}
		}
		out, err = resolvePolicy(tx, scope, planID)
		return err
	})
	if err != nil {
		return metering.PolicyResolution{}, err
	}
	return out, nil
}
func savePolicy(tx *gorm.DB, scope metering.PolicyScope, input metering.PolicyInput) error {
	var current PolicyRecord
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_type = ? AND scope_id = ?", scope.Type, scope.ID).First(&current).Error
	now := time.Now().UTC()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if input.ExpectedRevision != 0 {
			return metering.ErrPolicyRevisionConflict
		}
		saved := PolicyRecord(metering.PolicyFromInput(scope.Type, scope.ID, input, 1, now))
		saved.CreatedAt = now
		// Concurrent first saves must not overwrite the winning revision.
		err := tx.Create(&saved).Error
		var my *mysql.MySQLError
		var sq *sqlite.Error
		if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &my) && my.Number == 1062) || (errors.As(err, &sq) && (sq.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE || sq.Code() == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY)) {
			return metering.ErrPolicyRevisionConflict
		}
		return err
	}
	if err != nil {
		return err
	}
	if input.ExpectedRevision == 0 || input.ExpectedRevision != current.Revision {
		return metering.ErrPolicyRevisionConflict
	}
	saved := PolicyRecord(metering.PolicyFromInput(scope.Type, scope.ID, input, current.Revision+1, now))
	result := tx.Model(&PolicyRecord{}).Where("scope_type = ? AND scope_id = ? AND revision = ?", scope.Type, scope.ID, current.Revision).Select("enabled", "evaluation_interval_seconds", "connection_start_threshold", "connection_start_window_seconds", "connection_start_penalty", "working_node_threshold", "working_node_window_seconds", "working_node_penalty", "score_max", "recovery_per_interval", "warning_score", "violation_score", "enforcement_mode", "restriction_duration_seconds", "revision", "updated_at").Updates(&saved)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return metering.ErrPolicyRevisionConflict
	}
	return nil
}
func loadPolicy(tx *gorm.DB, scope string, id uint) (*metering.Policy, error) {
	var row PolicyRecord
	err := tx.Session(&gorm.Session{}).Where("scope_type = ? AND scope_id = ?", scope, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := metering.Policy(row)
	return &out, nil
}
func resolvePolicy(tx *gorm.DB, scope metering.PolicyScope, planID uint) (metering.PolicyResolution, error) {
	override, err := loadPolicy(tx, scope.Type, scope.ID)
	if err != nil {
		return metering.PolicyResolution{}, err
	}
	out := metering.PolicyResolution{Configured: override != nil, Override: override}
	var sub, plan, platform *metering.Policy
	if scope.Type == "platform" {
		platform = override
	} else {
		platform, err = loadPolicy(tx, "platform", 0)
		if err != nil {
			return out, err
		}
		if scope.Type == "plan" {
			plan = override
		} else {
			sub = override
			plan, err = loadPolicy(tx, "plan", planID)
			if err != nil {
				return out, err
			}
		}
	}
	out.Effective, out.Source = metering.ChoosePolicy(sub, plan, platform)
	return out, nil
}
