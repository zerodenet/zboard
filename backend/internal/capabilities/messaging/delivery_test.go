package messaging

import (
	"context"
	"errors"
	"testing"
)

type countingChannel struct{ calls int }

func (c *countingChannel) Send(context.Context, Message) error { c.calls++; return nil }
func (c *countingChannel) Check(context.Context) error         { c.calls++; return nil }
func TestDeliveryRejectsHeaderInjectionAndCanceledCalls(t *testing.T) {
	channel := &countingChannel{}
	service := Delivery{Channel: channel}
	message := Message{ID: "<message@example.test>\r\nBcc: victim@example.test", Recipient: "user@example.test", Subject: "subject", Body: "body"}
	if err := service.Send(context.Background(), message); !errors.Is(err, ErrInvalidMessage) {
		t.Fatal(err)
	}
	message.ID = "<message@example.test>"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Send(ctx, message); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if channel.calls != 0 {
		t.Fatal("invalid request reached channel")
	}
	if err := service.Send(context.Background(), message); err != nil || channel.calls != 1 {
		t.Fatal(err, channel.calls)
	}
}
