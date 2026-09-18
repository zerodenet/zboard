package observability

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrAuditNotFound = errors.New("audit log not found")

type AuditLogRecord struct {
	ID                            uint
	UserID                        *uint
	Actor, Action, Target, Detail string
	CreatedAt                     time.Time
}

type AuditLogQuery struct {
	Actor, Action, Target string
	From, To              time.Time
	CursorAt              time.Time
	CursorID              uint
	CursorDirection       string
	Offset, Limit         int
}

type AuditLogPage struct {
	Items   []AuditLogRecord
	Total   int64
	HasMore bool
}

type AuditDirectoryRepository interface {
	ListAuditLogs(context.Context, AuditLogQuery) (AuditLogPage, error)
	AuditLog(context.Context, uint) (AuditLogRecord, error)
}

type AuditDirectory struct{ Repository AuditDirectoryRepository }

func (s AuditDirectory) List(ctx context.Context, q AuditLogQuery) (AuditLogPage, error) {
	return s.Repository.ListAuditLogs(ctx, q)
}
func (s AuditDirectory) Detail(ctx context.Context, id uint) (AuditLogRecord, error) {
	if id == 0 {
		return AuditLogRecord{}, ErrAuditNotFound
	}
	return s.Repository.AuditLog(ctx, id)
}

var (
	ErrAuditUnavailable = errors.New("audit writer unavailable")
	ErrAuditPermission  = errors.New("audit writer requires administrator")
	ErrAuditInvalid     = errors.New("invalid audit event")
)

type AuditEvent struct {
	Action string
	Target string
	Detail string
}

type AuditRepository interface {
	RecordAdminAudit(context.Context, uint, AuditEvent) error
}

type Audit struct{ Repository AuditRepository }

func (s Audit) RecordAdmin(ctx context.Context, actor uint, event AuditEvent) error {
	event.Action = strings.TrimSpace(event.Action)
	event.Target = strings.TrimSpace(event.Target)
	if s.Repository == nil {
		return ErrAuditUnavailable
	}
	if actor == 0 {
		return ErrAuditPermission
	}
	if event.Action == "" || event.Target == "" || len(event.Action) > 128 || len(event.Target) > 128 {
		return ErrAuditInvalid
	}
	return s.Repository.RecordAdminAudit(ctx, actor, event)
}
