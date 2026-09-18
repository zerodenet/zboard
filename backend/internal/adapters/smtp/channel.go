package smtpadapter

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

type Channel struct{ Settings platform.SMTPSettings }

func (c Channel) Send(ctx context.Context, message messaging.Message) error {
	if err := platform.ValidateSMTPDeliverySettings(c.Settings, false); err != nil {
		return err
	}
	return send(ctx, c.Settings, message.Recipient, message.Subject, message.Body, message.ID)
}
func (c Channel) Check(ctx context.Context) error {
	if err := platform.ValidateSMTPDeliverySettings(c.Settings, false); err != nil {
		return err
	}
	return Check(ctx, c.Settings)
}
