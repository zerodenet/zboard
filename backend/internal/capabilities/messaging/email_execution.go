package messaging

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

type EmailExecutionClaim struct {
	TaskID, ItemID uint
	Token          string
}
type EmailRecipient struct {
	Email, AccountName string
	RegisteredAt       time.Time
}
type EmailTaskData struct {
	Content    EmailContent
	Recipient  EmailRecipient
	LeaseUntil time.Time
}
type EmailExecutionRepository interface {
	Prepare(context.Context, EmailExecutionClaim) (EmailTaskData, error)
	Check(context.Context, EmailExecutionClaim) error
}
type EmailTaskSender interface {
	SendTaskMessage(context.Context, Message) error
}
type EmailExecution struct {
	Repository EmailExecutionRepository
	Sender     EmailTaskSender
}

func (s EmailExecution) Execute(ctx context.Context, claim EmailExecutionClaim) error {
	if claim.TaskID == 0 || claim.ItemID == 0 || claim.Token == "" {
		return ErrInvalidMessage
	}
	data, err := s.Repository.Prepare(ctx, claim)
	if err != nil {
		return err
	}
	if !identity.ValidEmail(data.Recipient.Email) {
		return ErrInvalidMessage
	}
	deadline := time.Now().Add(20 * time.Second)
	if data.LeaseUntil.Before(deadline) {
		deadline = data.LeaseUntil
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	subject, body := RenderContent(data.Content.Subject, data.Content.Body, RecipientVariables(data.Recipient, data.Content.SiteName, data.Content.SiteURL, time.Now().UTC()))
	message := Message{ID: fmt.Sprintf("task-%d-item-%d", claim.TaskID, claim.ItemID), Recipient: data.Recipient.Email, Subject: subject, Body: body}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Sender.SendTaskMessage(ctx, message); err != nil {
		return err
	}
	// Sending and acknowledgement are different: a stale attempt cannot confirm
	// success even if its provider call returned after its lease was replaced.
	if err := s.Repository.Check(ctx, claim); err != nil {
		return fmt.Errorf("%w: %w", ErrAcceptanceUnknown, err)
	}
	return nil
}
func RecipientVariables(user EmailRecipient, siteName, siteURL string, now time.Time) map[string]string {
	if strings.TrimSpace(siteName) == "" {
		siteName = "Zboard"
	}
	name := strings.TrimSpace(user.AccountName)
	if name == "" {
		name = user.Email
	}
	registered := user.RegisteredAt.UTC()
	if registered.IsZero() {
		registered = now.UTC()
	}
	return map[string]string{"site_name": siteName, "site_url": strings.TrimSpace(siteURL), "user_email": user.Email, "account_name": name, "registered_at": registered.Format("2006-01-02 15:04 UTC"), "current_date": now.UTC().Format("2006-01-02")}
}
