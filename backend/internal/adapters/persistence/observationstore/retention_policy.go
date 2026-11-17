package observationstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"strconv"
	"time"
)

type materialRetentionPolicy struct {
	material, receipt time.Duration
	changedAt         time.Time
}

func (s MaterialRetention) withPolicy(ctx context.Context) (MaterialRetention, error) {
	p := materialRetentionPolicy{material: MaterialMinimumAge, receipt: completedReceiptAge}
	var rows []model.SystemConfig
	if e := s.DB.WithContext(ctx).Select("config_key,value,updated_at").Where("config_key IN ?", []string{observability.ObservationMaterialDaysKey, observability.ObservationReceiptDaysKey}).Find(&rows).Error; e != nil {
		return s, e
	}
	for _, r := range rows {
		if r.UpdatedAt.After(p.changedAt) {
			p.changedAt = r.UpdatedAt
		}
		n, e := strconv.Atoi(r.Value)
		if e != nil || n < 0 || n > observability.RetentionMaxDays {
			continue
		}
		age := time.Duration(n) * 24 * time.Hour
		if r.ConfigKey == observability.ObservationMaterialDaysKey {
			p.material = age
		} else {
			p.receipt = age
		}
	}
	s.policy = &p
	return s, nil
}
func (s MaterialRetention) materialAge() time.Duration {
	if s.policy != nil {
		return s.policy.material
	}
	return MaterialMinimumAge
}
func (s MaterialRetention) receiptAge() time.Duration {
	if s.policy != nil {
		return s.policy.receipt
	}
	return completedReceiptAge
}

func (s MaterialRetention) reviewPolicyChanged(next time.Time) bool {
	return s.policy != nil && s.policy.changedAt.After(next.Add(-time.Hour))
}
