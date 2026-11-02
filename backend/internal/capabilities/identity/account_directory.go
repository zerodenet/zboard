package identity

import (
	"context"
	"time"
)

type AccountDirectoryQuery struct {
	Status        string
	IsAdmin       *bool
	EmailVerified *bool
	Search        string
	Paged         bool
	Offset        int
	Limit         int
	Sort          string
	Direction     string
}

type AccountDirectoryItem struct {
	PublicAccount
	AccountOverview
	ActiveSubscriptionCount int64     `json:"active_subscription_count"`
	TotalSubscriptionCount  int64     `json:"total_subscription_count"`
	PendingOrderCount       int64     `json:"pending_order_count"`
	TotalOrderCount         int64     `json:"total_order_count"`
	CreatedAt               time.Time `json:"created_at"`
}

type AccountBusinessDetail struct {
	AccountOverview
	ID                      uint       `json:"id"`
	AccountName             string     `json:"account_name"`
	Email                   string     `json:"email"`
	EmailVerifiedAt         *time.Time `json:"email_verified_at"`
	LastLoginAt             *time.Time `json:"last_login_at"`
	IsAdmin                 bool       `json:"is_admin"`
	Status                  string     `json:"status"`
	ActiveSubscriptionCount int64      `json:"active_subscription_count"`
	TotalSubscriptionCount  int64      `json:"total_subscription_count"`
	PendingOrderCount       int64      `json:"pending_order_count"`
	TotalOrderCount         int64      `json:"total_order_count"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// Read-only, current business state. Dates are eligibility estimates, not a
// reservation for deletion: the worker rechecks policy and funds when it runs.
type AccountCleanupStatus struct {
	Enabled      bool       `json:"enabled"`
	InactiveDays int        `json:"inactive_days"`
	State        string     `json:"state"`
	EligibleAt   *time.Time `json:"eligible_at"`
	Reason       string     `json:"reason"`
}

type AccountWalletBalance struct {
	Currency      string `json:"currency"`
	BalanceMinor  string `json:"balance_minor"`
	ReservedMinor string `json:"reserved_minor"`
}

type AccountOverview struct {
	WalletBalances []AccountWalletBalance `json:"wallet_balances"`
	Cleanup        AccountCleanupStatus   `json:"cleanup"`
}

type AccountDirectoryPage struct {
	Items []AccountDirectoryItem
	Total int64
}

type AccountDirectoryRepository interface {
	List(context.Context, AccountDirectoryQuery, time.Time) (AccountDirectoryPage, error)
	Detail(context.Context, uint, time.Time) (AccountBusinessDetail, error)
}

type AccountDirectory struct{ Repository AccountDirectoryRepository }

func (s AccountDirectory) List(ctx context.Context, q AccountDirectoryQuery) (AccountDirectoryPage, error) {
	return s.Repository.List(ctx, q, time.Now().UTC())
}

func (s AccountDirectory) Detail(ctx context.Context, id uint) (AccountBusinessDetail, error) {
	if id == 0 {
		return AccountBusinessDetail{}, ErrAccountNotFound
	}
	return s.Repository.Detail(ctx, id, time.Now().UTC())
}
