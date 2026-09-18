package observability

import (
	"context"
	"errors"
	"time"
)

var ErrOperationLogNotFound = errors.New("operation log not found")

type OperationLogItem struct {
	ID                 uint       `json:"id"`
	Source             string     `json:"source"`
	Action             string     `json:"action"`
	Status             string     `json:"status"`
	TargetType         string     `json:"target_type"`
	TargetID           uint       `json:"target_id"`
	NodeID             uint       `json:"node_id,omitempty"`
	ProtocolEndpointID uint       `json:"protocol_endpoint_id,omitempty"`
	Summary            string     `json:"summary,omitempty"`
	HasOutput          bool       `json:"has_output"`
	HasError           bool       `json:"has_error"`
	Output             string     `json:"output,omitempty"`
	Error              string     `json:"error,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type OperationLogQuery struct {
	Source, Status                string
	NodeID, ProtocolEndpointID    uint
	From, To                      time.Time
	CursorAt                      time.Time
	CursorID                      uint
	CursorSource, CursorDirection string
	FetchLimit                    int
}

type OperationLogSourcePage struct {
	Items []OperationLogItem
	Total int64
}

type OperationLogRepository interface {
	ListSource(context.Context, OperationLogQuery) (OperationLogSourcePage, error)
	Detail(context.Context, string, uint) (OperationLogItem, error)
}

type OperationLogs struct{ Repository OperationLogRepository }

func (s OperationLogs) ListSource(ctx context.Context, q OperationLogQuery) (OperationLogSourcePage, error) {
	return s.Repository.ListSource(ctx, q)
}
func (s OperationLogs) Detail(ctx context.Context, source string, id uint) (OperationLogItem, error) {
	if id == 0 {
		return OperationLogItem{}, ErrOperationLogNotFound
	}
	return s.Repository.Detail(ctx, source, id)
}
