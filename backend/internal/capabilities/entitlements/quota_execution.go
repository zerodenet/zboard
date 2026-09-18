package entitlements

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"math"
	"strings"
)

type QuotaExecutionClaim struct {
	TaskID, ItemID uint
	Token          string
}
type QuotaExecutionRepository interface {
	Execute(context.Context, QuotaExecutionClaim) error
}
type QuotaExecution struct{ Repository QuotaExecutionRepository }

func (s QuotaExecution) Execute(ctx context.Context, claim QuotaExecutionClaim) error {
	if claim.TaskID == 0 || claim.ItemID == 0 || claim.Token == "" {
		return jobs.ErrLeaseLost
	}
	return s.Repository.Execute(ctx, claim)
}
func AdjustQuota(total, used int64, in QuotaAdjustment) (int64, int64, error) {
	if in.DeltaMB == 0 || in.DeltaMB < -1000000000 || in.DeltaMB > 1000000000 || len(strings.TrimSpace(in.Reason)) < 3 || len(in.Reason) > 255 {
		return 0, 0, ErrQuotaRequestInvalid
	}
	delta := in.DeltaMB * 1024 * 1024
	if total < 0 || used < 0 || (delta > 0 && total > math.MaxInt64-delta) || (delta < 0 && total < -delta) {
		return 0, 0, ErrQuotaRequestInvalid
	}
	next := total + delta
	if next < used {
		return 0, 0, ErrQuotaRequestInvalid
	}
	return next, delta, nil
}
