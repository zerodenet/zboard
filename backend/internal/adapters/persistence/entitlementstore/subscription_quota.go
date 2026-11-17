package entitlementstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionQuota struct {
	DB      *gorm.DB
	Issuer  entitlements.CredentialIssuer
	Publish func(*gorm.DB, uint, uint) error
}

func (s SubscriptionQuota) Update(ctx context.Context, actor uint, in entitlements.SubscriptionQuotaInput) error {
	if s.DB == nil || s.Issuer == nil || s.Publish == nil {
		return entitlements.ErrSubscriptionMutationUnavailable
	}
	encoded, err := json.Marshal(struct {
		Actor uint
		Input entitlements.SubscriptionQuotaInput
	}{actor, in})
	if err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	return RunCredentialTransaction(ctx, s.DB, func(tx *gorm.DB) error {
		if err := subscriptionReader(tx, actor, true); err != nil {
			return err
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, in.SubscriptionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return entitlements.ErrAccessNotFound
			}
			return err
		}
		var prior model.QuotaEvent
		err := tx.Where("subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?", sub.ID, "admin_quota", "admin_quota", in.IdempotencyKey).First(&prior).Error
		if err == nil {
			var detail struct {
				Hash string `json:"hash"`
			}
			if json.Unmarshal([]byte(prior.Detail), &detail) != nil || detail.Hash != hash {
				return entitlements.ErrSubscriptionQuotaConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if sub.Status == "ended" || sub.Status == "canceled" {
			return entitlements.ErrSubscriptionQuotaInvalid
		}
		now := time.Now().UTC()
		scheduleEdited := in.EndAt != nil || in.NextResetAt != nil || in.ResetPolicy != nil
		quotaEdited := in.FlowTotal != nil || in.FlowUsed != nil || in.ResetQuotaBytes != nil
		if !scheduleEdited && sub.EndedAt == nil && (sub.Status == "active" || sub.Status == "expired") && sub.EndAt.After(now) {
			if _, err := ApplyDueTrafficReset(tx, &sub, now); err != nil {
				return err
			}
		}
		total, used := entitlements.CycleQuota(entitlements.Subscription(sub))
		old := map[string]any{"flow_total": total, "flow_used": used, "reset_quota_bytes": sub.ResetQuotaBytes}

		if scheduleEdited {
			old["end_at"], old["next_reset_at"], old["reset_policy"], old["status"], old["ended_at"], old["end_reason"] = sub.EndAt, sub.NextResetAt, sub.ResetPolicy, sub.Status, sub.EndedAt, sub.EndReason
			if in.EndAt != nil && in.ExpectedEndAt != nil && !sub.EndAt.Equal(*in.ExpectedEndAt) {
				return entitlements.ErrSubscriptionScheduleChanged
			}
			if in.NextResetAt != nil && in.ExpectedNextResetAt != nil {
				expected := *in.ExpectedNextResetAt
				if expected == "" {
					if sub.NextResetAt != nil {
						return entitlements.ErrSubscriptionScheduleChanged
					}
				} else {
					date, _ := time.Parse(time.RFC3339Nano, expected)
					if sub.NextResetAt == nil || !sub.NextResetAt.Equal(date) {
						return entitlements.ErrSubscriptionScheduleChanged
					}
				}
			}
			if in.EndAt != nil {
				if !in.EndAt.UTC().Truncate(time.Millisecond).After(sub.StartAt) {
					return entitlements.ErrSubscriptionQuotaInvalid
				}
				sub.EndAt = in.EndAt.UTC().Truncate(time.Millisecond)
				if sub.EndAt.After(now) && sub.Status == "expired" && (sub.EndReason == "" || sub.EndReason == "expired") {
					sub.EndedAt = nil
					sub.EndReason = ""
					sub.Status = "active"
					sub.RetirementRetryAt = nil
					if in.NextResetAt == nil && sub.ResetPolicy >= 1 && sub.ResetPolicy <= 4 && (sub.NextResetAt == nil || !sub.NextResetAt.After(now)) {
						sub.NextResetAt = entitlements.NextTrafficResetAfter(sub.StartAt, sub.ResetPolicy, now)
					}
				}
			}
			if in.ResetPolicy != nil {
				changed, err := entitlements.ChangeTrafficResetSchedule(entitlements.Subscription(sub), *in.ResetPolicy, now)
				if err != nil {
					return err
				}
				sub.ResetPolicy, sub.NextResetAt = changed.ResetPolicy, changed.NextResetAt
			}
			if in.NextResetAt != nil {
				if sub.ResetPolicy < 1 || sub.ResetPolicy > 4 || sub.EndsOnQuotaExhaustion || entitlements.IsPerpetualEnd(sub.EndAt) || sub.EndedAt != nil {
					return entitlements.ErrSubscriptionResetPolicyNotApplicable
				}
				if !in.NextResetAt.UTC().Truncate(time.Millisecond).After(now) || in.NextResetAt.Before(sub.StartAt) || in.NextResetAt.After(sub.EndAt) {
					return entitlements.ErrSubscriptionQuotaInvalid
				}
				date := in.NextResetAt.UTC().Truncate(time.Millisecond)
				sub.NextResetAt = &date
			}
		}

		resetQuota := sub.ResetQuotaBytes
		if in.FlowTotal != nil {
			total = *in.FlowTotal
		}
		if in.FlowUsed != nil {
			used = *in.FlowUsed
		}
		if in.ResetQuotaBytes != nil {
			resetQuota = *in.ResetQuotaBytes
		}
		baseline := sub.CycleStartUsed
		if quotaEdited && (baseline < 0 || baseline > sub.FlowTotal || baseline > sub.FlowUsed || baseline > math.MaxInt64-max(total, used)) {
			return entitlements.ErrSubscriptionQuotaInvalid
		}
		before := sub.FlowTotal - sub.FlowUsed
		if quotaEdited {
			sub.FlowTotal, sub.FlowUsed = baseline+total, baseline+used
			sub.ResetQuotaBytes = resetQuota
			sub.Status = entitlements.EffectiveStatus(entitlements.Subscription(sub), now)
			// A legacy exhausted instance can be restored by a quota adjustment.
			if sub.EndedAt == nil && sub.Status == "expired" && sub.EndAt.After(now) {
				sub.Status = "active"
			}
		}
		if in.EndAt != nil {
			sub.Status = entitlements.EffectiveStatus(entitlements.Subscription(sub), now)
		}
		sub.UpdatedAt = now
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		if in.EndAt != nil {
			if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND current_scope=1", sub.ID).Updates(map[string]any{"expires_at": sub.EndAt, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if quotaEdited || in.EndAt != nil {
			if entitlements.AccessAvailable(entitlements.Subscription(sub), now) {
				if _, err := EnsureCredentials(tx, sub, s.Issuer); err != nil {
					return err
				}
			} else if err := ExpireSubscriptionCredentials(tx, sub.ID, now); err != nil {
				return err
			}
			if err := s.Publish(tx, sub.ID, actor); err != nil {
				return err
			}
		}
		updated := map[string]any{"flow_total": total, "flow_used": used, "reset_quota_bytes": resetQuota}
		if scheduleEdited {
			updated["end_at"], updated["next_reset_at"], updated["reset_policy"], updated["status"], updated["ended_at"], updated["end_reason"] = sub.EndAt, sub.NextResetAt, sub.ResetPolicy, sub.Status, sub.EndedAt, sub.EndReason
		}

		detail, err := json.Marshal(map[string]any{"hash": hash, "actor_id": actor, "reason": in.Reason, "before": old, "after": updated})
		if err != nil {
			return err
		}
		after := sub.FlowTotal - sub.FlowUsed
		if err := tx.Create(&model.QuotaEvent{SubscriptionID: sub.ID, EventType: "admin_quota", DeltaBytes: after - before, BalanceBefore: before, BalanceAfter: after, ReferenceType: "admin_quota", ReferenceID: in.IdempotencyKey, Detail: string(detail)}).Error; err != nil {
			return err
		}
		var admin model.User
		if err := tx.Select("id", "email").First(&admin, actor).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "subscription.quota.update", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: string(detail)}).Error
	})
}
