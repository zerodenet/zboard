package metering

import (
	"context"
	"errors"
	"time"
)

var ErrTrendPermission = errors.New("current trend access required")

type TrendBucket struct {
	Key              string
	StartUTC, EndUTC time.Time
}
type PrincipalTrendRow struct {
	Day               string
	Peak, SampleCount int64
}
type PrincipalTrendQuery struct {
	Administrative         bool
	UserID, SubscriptionID uint
	Buckets                []TrendBucket
}
type PrincipalTrendRepository interface {
	Read(context.Context, uint, PrincipalTrendQuery) ([]PrincipalTrendRow, error)
}
type PrincipalTrends struct{ Repository PrincipalTrendRepository }

func (s PrincipalTrends) Read(ctx context.Context, actor uint, q PrincipalTrendQuery) ([]PrincipalTrendRow, error) {
	if actor == 0 {
		return nil, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	if len(q.Buckets) == 0 || len(q.Buckets) > 366 {
		return nil, &PolicyValidation{Fields: map[string]string{"range": "must contain 1 to 366 days"}}
	}
	for i, b := range q.Buckets {
		if _, err := time.Parse("2006-01-02", b.Key); err != nil {
			return nil, &PolicyValidation{Fields: map[string]string{"range": "invalid day"}}
		}
		duration := b.EndUTC.Sub(b.StartUTC)
		if b.StartUTC.IsZero() || duration <= 0 || duration > 48*time.Hour || (i > 0 && (!b.StartUTC.Equal(q.Buckets[i-1].EndUTC) || b.Key <= q.Buckets[i-1].Key)) {
			return nil, &PolicyValidation{Fields: map[string]string{"range": "invalid or overlapping day buckets"}}
		}
	}
	return s.Repository.Read(ctx, actor, q)
}
