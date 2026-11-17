package metering

import (
	"context"
	"errors"
)

var ErrUsageProjectionPending = errors.New("流量统计正在更新，已结算用量和当前配额不受影响，请稍后重试统计查询。")

// UsageProjections advances auxiliary hourly/daily statistics after authoritative
// metering facts and their durable projection intents have committed.
type UsageProjections struct{ Repository UsageProjectionRepository }

type UsageProjectionRepository interface {
	HasPending(context.Context) (bool, error)
	Drain(context.Context, int) (int, error)
}

func (s UsageProjections) HasPending(ctx context.Context) (bool, error) {
	return s.Repository.HasPending(ctx)
}

func (s UsageProjections) Drain(ctx context.Context, limit int) (int, error) {
	return s.Repository.Drain(ctx, limit)
}
