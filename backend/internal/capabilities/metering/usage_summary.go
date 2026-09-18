package metering

import "context"

type UsageSummaryQuery struct {
	Administrative bool
	UserID         uint
}
type UsageSummarySnapshot struct {
	TotalUsedBytes int64  `json:"total_used_bytes"`
	RemainingBytes int64  `json:"remaining_bytes"`
	UsedBytesToday int64  `json:"used_bytes_today"`
	ScopeUser      uint   `json:"scope_user"`
	AsOf           string `json:"as_of"`
}
type UsageSummaryRepository interface {
	Read(context.Context, uint, UsageSummaryQuery) (UsageSummarySnapshot, error)
}
type UsageSummary struct{ Repository UsageSummaryRepository }

func (s UsageSummary) Read(ctx context.Context, actor uint, q UsageSummaryQuery) (UsageSummarySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return UsageSummarySnapshot{}, err
	}
	if actor == 0 {
		return UsageSummarySnapshot{}, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	return s.Repository.Read(ctx, actor, q)
}
