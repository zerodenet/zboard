package experience

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrTicketForbidden = errors.New("ticket access denied")
	ErrTicketClosed    = errors.New("ticket is closed")
)

type TicketActor struct {
	ID      uint
	IsAdmin bool
}

type NewTicket struct {
	TicketNo string
	Subject  string
	Category string
	Priority int16
	Body     string
}

type Ticket struct {
	ID            uint       `json:"id"`
	TicketNo      string     `json:"ticket_no"`
	UserID        uint       `json:"user_id"`
	Subject       string     `json:"subject"`
	Category      string     `json:"category"`
	Priority      int16      `json:"priority"`
	Status        string     `json:"status"`
	LastMessageAt time.Time  `json:"last_message_at"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	ClosedAt      *time.Time `json:"closed_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type TicketMessage struct {
	ID         uint      `json:"id"`
	TicketID   uint      `json:"ticket_id"`
	AuthorID   *uint     `json:"author_id"`
	AuthorRole string    `json:"author_role"`
	Type       string    `json:"type"`
	Body       string    `json:"body"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	CreatedAt  time.Time `json:"created_at"`
}

type TicketSummary struct {
	Ticket
	UserEmail    string `json:"user_email"`
	MessageCount int64  `json:"message_count"`
}

type TicketQuery struct {
	ActorID  uint
	Admin    bool
	Statuses []string
	Category string
	Search   string
	Offset   int
	Limit    int
}

type TicketPage struct {
	Items []TicketSummary
	Total int64
}
type TicketMessageView struct {
	TicketMessage
	AuthorEmail string `json:"author_email"`
}
type TicketDetail struct {
	Ticket           TicketSummary       `json:"ticket"`
	Messages         []TicketMessageView `json:"messages"`
	HasOlderMessages bool                `json:"has_older_messages"`
	OldestMessageID  uint                `json:"oldest_message_id,omitempty"`
}

type TicketRepository interface {
	CreateTicket(context.Context, uint, NewTicket, time.Time) (uint, error)
	ReplyTicket(context.Context, TicketActor, uint, string, time.Time) error
	ChangeTicketStatus(context.Context, TicketActor, uint, string, bool, time.Time) error
	ListTickets(context.Context, TicketQuery) (TicketPage, error)
	TicketDetail(context.Context, TicketActor, uint, uint, int) (TicketDetail, error)
}

type Tickets struct{ Repository TicketRepository }

func (s Tickets) Create(ctx context.Context, actor uint, input NewTicket, now time.Time) (uint, error) {
	if actor == 0 {
		return 0, ErrPermission
	}
	input.Subject = strings.TrimSpace(input.Subject)
	input.Category = strings.TrimSpace(input.Category)
	input.Body = strings.TrimSpace(input.Body)
	if err := validateTicketText("subject", input.Subject, 1, 160); err != nil {
		return 0, err
	}
	if !oneOf(input.Category, "connection", "billing", "account", "other") {
		return 0, fmt.Errorf("%w: invalid ticket category", ErrInvalid)
	}
	if input.Priority != 1 && input.Priority != 2 {
		return 0, fmt.Errorf("%w: priority must be 1 or 2", ErrInvalid)
	}
	if err := validateTicketText("message", input.Body, 1, 5000); err != nil {
		return 0, err
	}
	return s.Repository.CreateTicket(ctx, actor, input, now.UTC())
}

func (s Tickets) Reply(ctx context.Context, actor TicketActor, id uint, body string, now time.Time) error {
	if actor.ID == 0 {
		return ErrPermission
	}
	body = strings.TrimSpace(body)
	if err := validateTicketText("message", body, 1, 5000); err != nil {
		return err
	}
	return s.Repository.ReplyTicket(ctx, actor, id, body, now.UTC())
}

func (s Tickets) ChangeStatus(ctx context.Context, actor TicketActor, id uint, status string, adminOverride bool, now time.Time) error {
	if actor.ID == 0 {
		return ErrPermission
	}
	status = strings.TrimSpace(status)
	if !oneOf(status, "open", "pending_admin", "pending_user", "resolved", "closed") {
		return fmt.Errorf("%w: invalid ticket status", ErrInvalid)
	}
	return s.Repository.ChangeTicketStatus(ctx, actor, id, status, adminOverride, now.UTC())
}

func (s Tickets) List(ctx context.Context, query TicketQuery) (TicketPage, error) {
	if query.ActorID == 0 {
		return TicketPage{}, ErrPermission
	}
	return s.Repository.ListTickets(ctx, query)
}

func (s Tickets) Detail(ctx context.Context, actor TicketActor, id, beforeID uint, limit int) (TicketDetail, error) {
	if actor.ID == 0 {
		return TicketDetail{}, ErrPermission
	}
	return s.Repository.TicketDetail(ctx, actor, id, beforeID, limit)
}

func validateTicketText(field, value string, minimum, maximum int) error {
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return fmt.Errorf("%w: %s must contain between %d and %d characters", ErrInvalid, field, minimum, maximum)
	}
	return nil
}
