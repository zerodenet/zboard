package messaging

import (
	"context"
	"errors"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

var ErrInvalidMessage = errors.New("invalid message")

type Message struct{ ID, Recipient, Subject, Body string }

// Channel provides transport execution only. Recipient authorization, template
// selection and durable requests belong to the owning application service.
type Channel interface {
	Send(context.Context, Message) error
	Check(context.Context) error
}
type Delivery struct{ Channel Channel }

func (s Delivery) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !identity.ValidEmail(message.Recipient) || message.ID == "" || len(message.ID) > 512 || strings.ContainsAny(message.ID, "\r\n") || len(message.Subject) > 4096 || len(message.Body) > 1<<20 {
		return ErrInvalidMessage
	}
	return s.Channel.Send(ctx, message)
}
func (s Delivery) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Channel.Check(ctx)
}
