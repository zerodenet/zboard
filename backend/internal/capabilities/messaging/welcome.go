package messaging

import "context"

// EmailContent is the immutable template/site snapshot consumed by delivery.
// Recipient identity is resolved from the core account at execution time.
type EmailContent struct {
	Subject          string `json:"subject"`
	Body             string `json:"body"`
	TemplateID       uint   `json:"template_id,omitempty"`
	TemplateRevision uint64 `json:"template_revision,omitempty"`
	SiteName         string `json:"site_name,omitempty"`
	SiteURL          string `json:"site_url,omitempty"`
}
type WelcomeReceipt struct {
	TaskID  uint
	Created bool
}
type WelcomeRepository interface {
	EnqueueRegisteredAccount(context.Context, uint) (WelcomeReceipt, error)
}

// RegistrationWelcome is a trusted account-registration hook, not an arbitrary
// recipient/message entry point for user or plugin requests.
type RegistrationWelcome struct{ Repository WelcomeRepository }

type RegistrationEventRepository interface {
	ProcessRegistrationEvents(context.Context, int) (int, error)
}
type RegistrationEvents struct{ Repository RegistrationEventRepository }

func (s RegistrationEvents) Process(ctx context.Context) (int, error) {
	return s.Repository.ProcessRegistrationEvents(ctx, 20)
}

func (s RegistrationWelcome) Enqueue(ctx context.Context, accountID uint) (WelcomeReceipt, error) {
	if accountID == 0 {
		return WelcomeReceipt{}, ErrInvalidMessage
	}
	return s.Repository.EnqueueRegisteredAccount(ctx, accountID)
}
