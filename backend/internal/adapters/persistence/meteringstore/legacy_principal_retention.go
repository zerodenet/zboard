package meteringstore

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Legacy principal observations are no longer a source for accounting, replay
// admission, current connections or the scope statistics projector. Generation
// and current snapshot floors remain in their own tables. Do not touch those.
type LegacyPrincipalRetention struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s LegacyPrincipalRetention) cutoff() time.Time {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	return now.Add(-PrincipalDiagnosticHorizon)
}

func (s LegacyPrincipalRetention) HasPending(ctx context.Context) (bool, error) {
	var first PrincipalFlowObservation
	r := s.DB.WithContext(ctx).Select("id,created_at").Order("id").Limit(1).Find(&first)
	return r.Error == nil && r.RowsAffected > 0 && first.CreatedAt.Before(s.cutoff()), r.Error
}

func (s LegacyPrincipalRetention) Drain(ctx context.Context, limit int) (int, error) {
	if ctx == nil || s.DB == nil || limit < 1 || limit > 1000 {
		return 0, errors.New("legacy observation retention requires 1..1000 rows")
	}
	cutoff := s.cutoff()
	removed := int64(0)
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []PrincipalFlowObservation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,created_at").Order("id").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uint64, 0, len(rows))
		for _, row := range rows {
			if row.CreatedAt.Before(cutoff) {
				ids = append(ids, row.ID)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		r := tx.Where("id IN ? AND created_at < ?", ids, cutoff).Delete(&PrincipalFlowObservation{})
		removed = r.RowsAffected
		return r.Error
	})
	return int(removed), err
}
