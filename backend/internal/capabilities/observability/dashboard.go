package observability

import (
	"context"
	"errors"
	"time"
)

var ErrDashboardInvalid = errors.New("invalid dashboard query")

type DashboardPeriod struct {
	Range        string    `json:"range"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	PreviousFrom time.Time `json:"previous_from"`
	PreviousTo   time.Time `json:"previous_to"`
	Bucket       string    `json:"bucket"`
	Timezone     string    `json:"timezone"`
}

type DashboardBusiness struct {
	RevenueCents             int64  `json:"revenue_cents"`
	PreviousRevenueCents     int64  `json:"previous_revenue_cents"`
	PaidOrders               int64  `json:"paid_orders"`
	PreviousPaidOrders       int64  `json:"previous_paid_orders"`
	NewOrders                int64  `json:"new_orders"`
	RenewOrders              int64  `json:"renew_orders"`
	NewSubscriptions         int64  `json:"new_subscriptions"`
	PreviousNewSubscriptions int64  `json:"previous_new_subscriptions"`
	ActiveSubscriptions      int64  `json:"active_subscriptions"`
	ExpiringWithin3Days      int64  `json:"expiring_within_3d"`
	Currency                 string `json:"currency,omitempty"`
	MixedCurrency            bool   `json:"mixed_currency"`
}

type DashboardService struct {
	ActiveSubscriptions *int64 `json:"active_subscriptions"`
	ActiveFlows         *int64 `json:"active_flows"`
	TrafficBytes        int64  `json:"traffic_bytes"`
	OnlineNodes         int64  `json:"online_nodes"`
	EnabledNodes        int64  `json:"enabled_nodes"`
}

type DashboardSubscriptionHealth struct {
	ExpiringWithin24Hours int64 `json:"expiring_within_24h"`
	ExpiringWithin3Days   int64 `json:"expiring_within_3d"`
	ExpiringWithin7Days   int64 `json:"expiring_within_7d"`
	QuotaExhausted        int64 `json:"quota_exhausted"`
}

type DashboardAttention struct {
	OfflineNodes          int64 `json:"nodes_offline"`
	UnresolvedDeployments int64 `json:"deployments_unresolved"`
	PendingTickets        int64 `json:"tickets_pending"`
}

type DashboardInfrastructure struct {
	NodesTotal              int64 `json:"nodes_total"`
	NodesEnabled            int64 `json:"nodes_enabled"`
	ConnectorOnline         int64 `json:"connector_online"`
	SSHVerified             int64 `json:"ssh_verified"`
	TrafficReady            int64 `json:"traffic_ready"`
	ProtocolEndpoints       int64 `json:"protocol_endpoints"`
	ActiveProtocolEndpoints int64 `json:"active_protocol_endpoints"`
	PublishedPlans          int64 `json:"published_plans"`
	UnresolvedDeployments   int64 `json:"unresolved_deployments"`
}

type DashboardCoverage struct {
	PrincipalFlows bool `json:"principal_flows"`
}

type DashboardTrendBucket struct {
	Key      string
	StartUTC time.Time
	EndUTC   time.Time
}

type DashboardTrendPoint struct {
	BucketStart  time.Time `json:"bucket_start"`
	RevenueCents int64     `json:"revenue_cents"`
	PaidOrders   int64     `json:"paid_orders"`
	NewOrders    int64     `json:"new_orders"`
	RenewOrders  int64     `json:"renew_orders"`
}

type DashboardSnapshot struct {
	Business       DashboardBusiness
	Service        DashboardService
	Subscriptions  DashboardSubscriptionHealth
	Attention      DashboardAttention
	Infrastructure DashboardInfrastructure
	Coverage       DashboardCoverage
	Trend          []DashboardTrendPoint
}

type DashboardTotals struct {
	Users                   int64 `json:"users"`
	ActiveUsers             int64 `json:"users_active"`
	Nodes                   int64 `json:"nodes"`
	Plans                   int64 `json:"plans"`
	Orders                  int64 `json:"orders"`
	PaidOrders              int64 `json:"orders_paid"`
	PendingOrders           int64 `json:"orders_pending"`
	FailedOrders            int64 `json:"orders_failed"`
	RevenueCents            int64 `json:"revenue_cents"`
	Subscriptions           int64 `json:"subscriptions"`
	ActiveSubscriptions     int64 `json:"subscriptions_active"`
	TrafficPoolBytes        int64 `json:"traffic_pool_bytes"`
	OfflineNodes            int64 `json:"nodes_offline"`
	PendingTickets          int64 `json:"tickets_pending"`
	FailedTasks             int64 `json:"tasks_failed"`
	FailedDeployments       int64 `json:"deployments_failed"`
	ConnectorOnlineNodes    int64 `json:"nodes_connector_online"`
	SSHVerifiedNodes        int64 `json:"nodes_ssh_verified"`
	TrafficReadyNodes       int64 `json:"nodes_traffic_ready"`
	ProtocolEndpoints       int64 `json:"protocol_endpoints"`
	ActiveProtocolEndpoints int64 `json:"protocol_endpoints_active"`
}

type DashboardRepository interface {
	LoadDashboard(context.Context, DashboardPeriod, time.Time, []DashboardTrendBucket) (DashboardSnapshot, error)
	LoadDashboardTotals(context.Context, time.Time) (DashboardTotals, error)
}

type Dashboard struct{ Repository DashboardRepository }

func (s Dashboard) Load(ctx context.Context, period DashboardPeriod, now time.Time, buckets []DashboardTrendBucket) (DashboardSnapshot, error) {
	if s.Repository == nil || period.From.IsZero() || period.To.Before(period.From) || period.PreviousFrom.IsZero() || period.PreviousTo.Before(period.PreviousFrom) {
		return DashboardSnapshot{}, ErrDashboardInvalid
	}
	return s.Repository.LoadDashboard(ctx, period, now.UTC(), buckets)
}

func (s Dashboard) Totals(ctx context.Context, now time.Time) (DashboardTotals, error) {
	if s.Repository == nil || now.IsZero() {
		return DashboardTotals{}, ErrDashboardInvalid
	}
	return s.Repository.LoadDashboardTotals(ctx, now.UTC())
}
