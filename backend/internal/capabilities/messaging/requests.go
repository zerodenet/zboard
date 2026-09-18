package messaging

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"strings"
)

const MaxRequestRecipients = 10000

var ErrRequestConflict = errors.New("message request idempotency key already exists")

type RecipientScope struct {
	UserIDs   []uint `json:"user_ids,omitempty"`
	AllActive bool   `json:"all_active,omitempty"`
}
type RequestInput struct {
	Scope          RecipientScope
	Content        EmailContent
	IdempotencyKey string
	Priority       int
	MaxAttempts    int
	AutoRun        bool
}
type RequestReceipt = jobs.BatchReceipt

type RequestRepository interface {
	Create(context.Context, uint, RequestInput) (RequestReceipt, error)
}
type Requests struct{ Repository RequestRepository }

func (s Requests) Create(ctx context.Context, actor uint, in RequestInput) (RequestReceipt, error) {
	if actor == 0 {
		return RequestReceipt{}, ErrTemplatePermission
	}
	in.Content.Subject = strings.TrimSpace(in.Content.Subject)
	if in.Content.Subject == "" || len(in.Content.Subject) > 200 || strings.ContainsAny(in.Content.Subject, "\r\n") || strings.TrimSpace(in.Content.Body) == "" || len(in.Content.Body) > 100000 {
		return RequestReceipt{}, ErrInvalidMessage
	}
	if len(in.Scope.UserIDs) > MaxRequestRecipients {
		return RequestReceipt{}, ErrInvalidMessage
	}
	seen := map[uint]bool{}
	ids := []uint{}
	for _, id := range in.Scope.UserIDs {
		if id != 0 && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	in.Scope.UserIDs = ids
	if !in.Scope.AllActive && len(ids) == 0 {
		return RequestReceipt{}, ErrInvalidMessage
	}
	if in.MaxAttempts == 0 {
		in.MaxAttempts = 3
	}
	if in.MaxAttempts < 1 || in.MaxAttempts > 10 || in.Priority < -100 || in.Priority > 100 {
		return RequestReceipt{}, ErrInvalidMessage
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = uuid.NewString()
	}
	if len(in.IdempotencyKey) > 128 {
		return RequestReceipt{}, ErrInvalidMessage
	}
	// Site identity is always derived by the owning store.
	in.Content.SiteName = ""
	in.Content.SiteURL = ""
	return s.Repository.Create(ctx, actor, in)
}
