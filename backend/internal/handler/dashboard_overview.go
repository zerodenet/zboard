package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	dashboardRangeToday  = "today"
	dashboardRange7Days  = "7d"
	dashboardRange30Days = "30d"
)

type dashboardPeriod struct {
	Range        string    `json:"range"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	PreviousFrom time.Time `json:"previous_from"`
	PreviousTo   time.Time `json:"previous_to"`
	Bucket       string    `json:"bucket"`
	Timezone     string    `json:"timezone"`
}

type dashboardPeriodAggregate struct {
	RevenueCents int64 `json:"revenue_cents" gorm:"column:revenue_cents"`
	PaidOrders   int64 `json:"paid_orders" gorm:"column:paid_orders"`
	NewOrders    int64 `json:"new_orders" gorm:"column:new_orders"`
	RenewOrders  int64 `json:"renew_orders" gorm:"column:renew_orders"`
}

type dashboardBusinessOverview struct {
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

type dashboardServiceOverview struct {
	ActiveSubscriptions *int64 `json:"active_subscriptions"`
	ActiveFlows         *int64 `json:"active_flows"`
	TrafficBytes        int64  `json:"traffic_bytes"`
	OnlineNodes         int64  `json:"online_nodes"`
	EnabledNodes        int64  `json:"enabled_nodes"`
}

type dashboardSubscriptionHealth struct {
	ExpiringWithin24Hours int64 `json:"expiring_within_24h"`
	ExpiringWithin3Days   int64 `json:"expiring_within_3d"`
	ExpiringWithin7Days   int64 `json:"expiring_within_7d"`
	QuotaExhausted        int64 `json:"quota_exhausted"`
}

type dashboardAttentionOverview struct {
	OfflineNodes          int64 `json:"nodes_offline"`
	UnresolvedDeployments int64 `json:"deployments_unresolved"`
	PendingTickets        int64 `json:"tickets_pending"`
}

type dashboardInfrastructureOverview struct {
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

type dashboardCoverage struct {
	PrincipalFlows bool `json:"principal_flows"`
}

type dashboardTrendPoint struct {
	BucketStart  time.Time `json:"bucket_start"`
	RevenueCents int64     `json:"revenue_cents"`
	PaidOrders   int64     `json:"paid_orders"`
	NewOrders    int64     `json:"new_orders"`
	RenewOrders  int64     `json:"renew_orders"`
}

type dashboardTrendRow struct {
	BucketStart  string `gorm:"column:bucket_start"`
	RevenueCents int64  `gorm:"column:revenue_cents"`
	PaidOrders   int64  `gorm:"column:paid_orders"`
	NewOrders    int64  `gorm:"column:new_orders"`
	RenewOrders  int64  `gorm:"column:renew_orders"`
}

type dashboardOverviewResponse struct {
	Period         dashboardPeriod                 `json:"period"`
	Business       dashboardBusinessOverview       `json:"business"`
	Service        dashboardServiceOverview        `json:"service"`
	Subscriptions  dashboardSubscriptionHealth     `json:"subscriptions"`
	Attention      dashboardAttentionOverview      `json:"attention"`
	Infrastructure dashboardInfrastructureOverview `json:"infrastructure"`
	Coverage       dashboardCoverage               `json:"coverage"`
	Trend          []dashboardTrendPoint           `json:"trend"`
	AsOf           time.Time                       `json:"as_of"`
}

// DashboardOverviewHandler is the period-aware operations read model used by
// the admin dashboard. Historical execution failures deliberately do not feed
// Attention; each attention counter represents a condition that is unresolved
// in current domain state.
func (h *handlers) DashboardOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	now := time.Now().UTC()
	period, err := resolveDashboardPeriod(r.URL.Query().Get("range"), now)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	business, err := h.loadDashboardBusiness(period, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	service, coverage, err := h.loadDashboardService(period, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	subscriptions, err := h.loadDashboardSubscriptionHealth(now)
	if err != nil {
		ServerError(w, err)
		return
	}
	attention, infrastructure, err := h.loadDashboardOperationalHealth(now)
	if err != nil {
		ServerError(w, err)
		return
	}
	trend, err := h.loadDashboardTrend(period)
	if err != nil {
		ServerError(w, err)
		return
	}

	OK(w, dashboardOverviewResponse{
		Period:         period,
		Business:       business,
		Service:        service,
		Subscriptions:  subscriptions,
		Attention:      attention,
		Infrastructure: infrastructure,
		Coverage:       coverage,
		Trend:          trend,
		AsOf:           now,
	})
}

func resolveDashboardPeriod(raw string, now time.Time) (dashboardPeriod, error) {
	now = now.UTC()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	period := dashboardPeriod{Range: strings.ToLower(strings.TrimSpace(raw)), To: now, Bucket: "day", Timezone: "UTC"}
	if period.Range == "" {
		period.Range = dashboardRange7Days
	}
	comparisonShiftDays := 0
	switch period.Range {
	case dashboardRangeToday:
		period.From = startOfToday
		period.Bucket = "hour"
		comparisonShiftDays = 1
	case dashboardRange7Days:
		period.From = startOfToday.AddDate(0, 0, -6)
		comparisonShiftDays = 7
	case dashboardRange30Days:
		period.From = startOfToday.AddDate(0, 0, -29)
		comparisonShiftDays = 30
	default:
		return dashboardPeriod{}, fmt.Errorf("range must be one of today, 7d, or 30d")
	}
	period.PreviousFrom = period.From.AddDate(0, 0, -comparisonShiftDays)
	period.PreviousTo = period.To.AddDate(0, 0, -comparisonShiftDays)
	return period, nil
}

func (h *handlers) loadDashboardBusiness(period dashboardPeriod, now time.Time) (dashboardBusinessOverview, error) {
	current, err := h.dashboardOrderAggregate(period.From, period.To)
	if err != nil {
		return dashboardBusinessOverview{}, err
	}
	previous, err := h.dashboardOrderAggregate(period.PreviousFrom, period.PreviousTo)
	if err != nil {
		return dashboardBusinessOverview{}, err
	}
	newSubscriptions, err := h.dashboardSubscriptionCreations(period.From, period.To)
	if err != nil {
		return dashboardBusinessOverview{}, err
	}
	previousNewSubscriptions, err := h.dashboardSubscriptionCreations(period.PreviousFrom, period.PreviousTo)
	if err != nil {
		return dashboardBusinessOverview{}, err
	}
	var activeSubscriptions int64
	if err := h.db.Model(&model.Subscription{}).
		Where("status = ? AND end_at > ? AND flow_used < flow_total", subStatusActive, now).
		Count(&activeSubscriptions).Error; err != nil {
		return dashboardBusinessOverview{}, err
	}
	var expiringWithin3Days int64
	if err := h.db.Model(&model.Subscription{}).
		Where("status = ? AND end_at > ? AND end_at <= ? AND flow_used < flow_total", subStatusActive, now, now.Add(72*time.Hour)).
		Count(&expiringWithin3Days).Error; err != nil {
		return dashboardBusinessOverview{}, err
	}
	currencyFrom := period.PreviousFrom
	if period.From.Before(currencyFrom) {
		currencyFrom = period.From
	}
	currency, mixed, err := h.dashboardRevenueCurrency(currencyFrom, period.To)
	if err != nil {
		return dashboardBusinessOverview{}, err
	}
	return dashboardBusinessOverview{
		RevenueCents:             current.RevenueCents,
		PreviousRevenueCents:     previous.RevenueCents,
		PaidOrders:               current.PaidOrders,
		PreviousPaidOrders:       previous.PaidOrders,
		NewOrders:                current.NewOrders,
		RenewOrders:              current.RenewOrders,
		NewSubscriptions:         newSubscriptions,
		PreviousNewSubscriptions: previousNewSubscriptions,
		ActiveSubscriptions:      activeSubscriptions,
		ExpiringWithin3Days:      expiringWithin3Days,
		Currency:                 currency,
		MixedCurrency:            mixed,
	}, nil
}

func (h *handlers) dashboardOrderAggregate(from, to time.Time) (dashboardPeriodAggregate, error) {
	var aggregate dashboardPeriodAggregate
	err := h.db.Model(&model.Order{}).
		Select(`
			COALESCE(SUM((CASE WHEN paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount), 0) AS revenue_cents,
			COUNT(*) AS paid_orders,
			COALESCE(SUM(CASE WHEN order_type = 'new' THEN 1 ELSE 0 END), 0) AS new_orders,
			COALESCE(SUM(CASE WHEN order_type = 'renew' THEN 1 ELSE 0 END), 0) AS renew_orders`).
		Where("status = ? AND paid_at IS NOT NULL AND paid_at >= ? AND paid_at < ?", orderStatusPaid, from, to).
		Scan(&aggregate).Error
	return aggregate, err
}

func (h *handlers) dashboardSubscriptionCreations(from, to time.Time) (int64, error) {
	var count int64
	err := h.db.Model(&model.Subscription{}).
		Where("created_at >= ? AND created_at < ?", from, to).
		Count(&count).Error
	return count, err
}

func (h *handlers) dashboardRevenueCurrency(from, to time.Time) (string, bool, error) {
	currencies := make([]string, 0, 2)
	if err := h.db.Model(&model.Order{}).
		Distinct("currency").
		Where("status = ? AND paid_at IS NOT NULL AND paid_at >= ? AND paid_at < ?", orderStatusPaid, from, to).
		Pluck("currency", &currencies).Error; err != nil {
		return "", false, err
	}
	clean := make([]string, 0, len(currencies))
	for _, currency := range currencies {
		currency = strings.ToUpper(strings.TrimSpace(currency))
		if currency != "" {
			clean = append(clean, currency)
		}
	}
	if len(clean) == 0 {
		return "CNY", false, nil
	}
	if len(clean) == 1 {
		return clean[0], false, nil
	}
	return "", true, nil
}

func (h *handlers) loadDashboardService(period dashboardPeriod, now time.Time) (dashboardServiceOverview, dashboardCoverage, error) {
	service := dashboardServiceOverview{}
	coverage := dashboardCoverage{}
	if err := h.db.Model(&model.TrafficRecord{}).
		Where("record_at >= ? AND record_at < ?", period.From, period.To).
		Select("COALESCE(SUM(used_bytes), 0)").Scan(&service.TrafficBytes).Error; err != nil {
		return service, coverage, err
	}
	if err := h.db.Model(&model.Node{}).Where("is_enabled = ?", true).Count(&service.EnabledNodes).Error; err != nil {
		return service, coverage, err
	}
	if err := h.db.Model(&model.Node{}).
		Where("is_enabled = ? AND connector_last_seen_at >= ?", true, now.Add(-nodeOnlineWindow)).
		Count(&service.OnlineNodes).Error; err != nil {
		return service, coverage, err
	}

	var observedScopes int64
	if err := h.db.Table("principal_flow_scope_currents").
		Where("scope_type = ?", principalFlowScopeSubscription).
		Count(&observedScopes).Error; err != nil {
		return service, coverage, err
	}
	coverage.PrincipalFlows = observedScopes > 0
	if coverage.PrincipalFlows {
		var activeSubscriptions int64
		if err := h.db.Table("principal_flow_scope_currents").
			Where("scope_type = ? AND active_flows > 0", principalFlowScopeSubscription).
			Count(&activeSubscriptions).Error; err != nil {
			return service, coverage, err
		}
		var activeFlows int64
		if err := h.db.Table("principal_flow_scope_currents").
			Where("scope_type = ?", principalFlowScopeSubscription).
			Select("COALESCE(SUM(active_flows), 0)").Scan(&activeFlows).Error; err != nil {
			return service, coverage, err
		}
		service.ActiveSubscriptions = &activeSubscriptions
		service.ActiveFlows = &activeFlows
	}
	return service, coverage, nil
}

func (h *handlers) loadDashboardSubscriptionHealth(now time.Time) (dashboardSubscriptionHealth, error) {
	health := dashboardSubscriptionHealth{}
	countExpiring := func(until time.Time, target *int64) error {
		return h.db.Model(&model.Subscription{}).
			Where("status = ? AND end_at > ? AND end_at <= ? AND flow_used < flow_total", subStatusActive, now, until).
			Count(target).Error
	}
	if err := countExpiring(now.Add(24*time.Hour), &health.ExpiringWithin24Hours); err != nil {
		return health, err
	}
	if err := countExpiring(now.Add(72*time.Hour), &health.ExpiringWithin3Days); err != nil {
		return health, err
	}
	if err := countExpiring(now.Add(7*24*time.Hour), &health.ExpiringWithin7Days); err != nil {
		return health, err
	}
	if err := h.db.Model(&model.Subscription{}).
		Where("end_at > ? AND flow_total > 0 AND flow_used >= flow_total AND status IN ?", now, []string{subStatusActive, subStatusExpired}).
		Count(&health.QuotaExhausted).Error; err != nil {
		return health, err
	}
	return health, nil
}

func (h *handlers) loadDashboardOperationalHealth(now time.Time) (dashboardAttentionOverview, dashboardInfrastructureOverview, error) {
	attention := dashboardAttentionOverview{}
	infrastructure := dashboardInfrastructureOverview{}

	if err := h.db.Model(&model.Node{}).Count(&infrastructure.NodesTotal).Error; err != nil {
		return attention, infrastructure, err
	}
	if err := h.db.Model(&model.Node{}).Where("is_enabled = ?", true).Count(&infrastructure.NodesEnabled).Error; err != nil {
		return attention, infrastructure, err
	}
	if err := h.db.Model(&model.Node{}).
		Where("is_enabled = ? AND connector_last_seen_at >= ?", true, now.Add(-nodeOnlineWindow)).
		Count(&infrastructure.ConnectorOnline).Error; err != nil {
		return attention, infrastructure, err
	}
	attention.OfflineNodes = infrastructure.NodesEnabled - infrastructure.ConnectorOnline
	if attention.OfflineNodes < 0 {
		attention.OfflineNodes = 0
	}
	if err := h.db.Model(&model.Node{}).
		Where("ssh_verified_at IS NOT NULL AND ssh_host_key_fingerprint <> ''").
		Count(&infrastructure.SSHVerified).Error; err != nil {
		return attention, infrastructure, err
	}
	if err := h.db.Model(&model.Node{}).
		Where("traffic_secret_prefix <> '' AND traffic_secret_revoked_at IS NULL").
		Count(&infrastructure.TrafficReady).Error; err != nil {
		return attention, infrastructure, err
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Count(&infrastructure.ProtocolEndpoints).Error; err != nil {
		return attention, infrastructure, err
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("is_active = ?", true).Count(&infrastructure.ActiveProtocolEndpoints).Error; err != nil {
		return attention, infrastructure, err
	}
	if err := h.db.Model(&model.Plan{}).Where("is_active = ?", true).Count(&infrastructure.PublishedPlans).Error; err != nil {
		return attention, infrastructure, err
	}
	if err := h.db.Model(&model.Ticket{}).
		Where("status IN ?", []string{ticketStatusOpen, ticketStatusPendingAdmin}).
		Count(&attention.PendingTickets).Error; err != nil {
		return attention, infrastructure, err
	}

	latestDeploymentIDs := h.db.Model(&model.ProtocolDeployment{}).
		Select("MAX(id)").Group("protocol_endpoint_id")
	if err := h.db.Model(&model.ProtocolDeployment{}).
		Where("id IN (?) AND status = ?", latestDeploymentIDs, "failed").
		Count(&attention.UnresolvedDeployments).Error; err != nil {
		return attention, infrastructure, err
	}
	infrastructure.UnresolvedDeployments = attention.UnresolvedDeployments
	return attention, infrastructure, nil
}

func (h *handlers) loadDashboardTrend(period dashboardPeriod) ([]dashboardTrendPoint, error) {
	bucketExpression := "DATE_FORMAT(paid_at, '%Y-%m-%d 00:00:00')"
	if period.Bucket == "hour" {
		bucketExpression = "DATE_FORMAT(paid_at, '%Y-%m-%d %H:00:00')"
	}
	if datastore.IsSQLite(h.db) {
		bucketExpression = "strftime('%Y-%m-%d 00:00:00', paid_at)"
		if period.Bucket == "hour" {
			bucketExpression = "strftime('%Y-%m-%d %H:00:00', paid_at)"
		}
	}
	var rows []dashboardTrendRow
	query := h.db.Model(&model.Order{}).
		Select(fmt.Sprintf(`%s AS bucket_start,
			COALESCE(SUM((CASE WHEN paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount), 0) AS revenue_cents,
			COUNT(*) AS paid_orders,
			COALESCE(SUM(CASE WHEN order_type = 'new' THEN 1 ELSE 0 END), 0) AS new_orders,
			COALESCE(SUM(CASE WHEN order_type = 'renew' THEN 1 ELSE 0 END), 0) AS renew_orders`, bucketExpression)).
		Where("status = ? AND paid_at IS NOT NULL AND paid_at >= ? AND paid_at < ?", orderStatusPaid, period.From, period.To).
		Group("bucket_start").Order("bucket_start ASC")
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return buildDashboardTrendPoints(period, rows), nil
}

func buildDashboardTrendPoints(period dashboardPeriod, rows []dashboardTrendRow) []dashboardTrendPoint {
	points := make([]dashboardTrendPoint, 0, 30)
	byBucket := make(map[string]dashboardTrendRow, len(rows))
	for _, row := range rows {
		key := normalizeDashboardBucketKey(period.Bucket, row.BucketStart)
		if key != "" {
			byBucket[key] = row
		}
	}
	step := time.Hour
	cursor := period.From.Truncate(time.Hour)
	end := period.To.Truncate(time.Hour)
	if period.Bucket != "hour" {
		step = 24 * time.Hour
		cursor = time.Date(period.From.Year(), period.From.Month(), period.From.Day(), 0, 0, 0, 0, time.UTC)
		end = time.Date(period.To.Year(), period.To.Month(), period.To.Day(), 0, 0, 0, 0, time.UTC)
	}
	for !cursor.After(end) {
		key := dashboardBucketKey(period.Bucket, cursor)
		row := byBucket[key]
		points = append(points, dashboardTrendPoint{
			BucketStart:  cursor,
			RevenueCents: row.RevenueCents,
			PaidOrders:   row.PaidOrders,
			NewOrders:    row.NewOrders,
			RenewOrders:  row.RenewOrders,
		})
		cursor = cursor.Add(step)
	}
	return points
}

func dashboardBucketKey(bucket string, value time.Time) string {
	value = value.UTC()
	if bucket == "hour" {
		return value.Format("2006-01-02 15:00:00")
	}
	return value.Format("2006-01-02 00:00:00")
}

func normalizeDashboardBucketKey(bucket, raw string) string {
	raw = strings.TrimSpace(raw)
	layouts := []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02"}
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, raw, time.UTC)
		if err == nil {
			return dashboardBucketKey(bucket, parsed)
		}
	}
	return ""
}
