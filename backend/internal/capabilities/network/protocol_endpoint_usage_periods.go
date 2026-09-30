package network

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrProtocolEndpointUsagePermission = errors.New("administrator access required")
	ErrProtocolEndpointUsageNotFound   = errors.New("protocol endpoint not found")
)

type ProtocolEndpointUsagePeriodValidation struct{ Message string }

func (e *ProtocolEndpointUsagePeriodValidation) Error() string { return e.Message }

type ProtocolEndpointUsageResetRecord struct {
	ID                 uint      `json:"id"`
	ProtocolEndpointID uint      `json:"protocol_endpoint_id"`
	ActorUserID        uint      `json:"actor_user_id"`
	ResetAt            time.Time `json:"reset_at"`
	PeriodUsedBytes    int64     `json:"period_used_bytes"`
	Reason             string    `json:"reason"`
}

type ProtocolEndpointUsagePeriodRepository interface {
	ResetProtocolEndpointUsage(context.Context, uint, uint, string, time.Time) (ProtocolEndpointUsageResetRecord, error)
	ListProtocolEndpointUsageResets(context.Context, uint, uint, int) ([]ProtocolEndpointUsageResetRecord, error)
}

type ProtocolEndpointUsagePeriods struct {
	Repository ProtocolEndpointUsagePeriodRepository
}

func (s ProtocolEndpointUsagePeriods) Reset(ctx context.Context, actor, endpointID uint, reason string) (ProtocolEndpointUsageResetRecord, error) {
	reason = strings.TrimSpace(reason)
	if endpointID == 0 || reason == "" || len(reason) > 255 {
		return ProtocolEndpointUsageResetRecord{}, &ProtocolEndpointUsagePeriodValidation{Message: "请填写 1–255 字节的统计重置原因。"}
	}
	return s.Repository.ResetProtocolEndpointUsage(ctx, actor, endpointID, reason, time.Now().UTC())
}

func (s ProtocolEndpointUsagePeriods) History(ctx context.Context, actor, endpointID uint) ([]ProtocolEndpointUsageResetRecord, error) {
	if endpointID == 0 {
		return nil, ErrProtocolEndpointUsageNotFound
	}
	return s.Repository.ListProtocolEndpointUsageResets(ctx, actor, endpointID, 20)
}
