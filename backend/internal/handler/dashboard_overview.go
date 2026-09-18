package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
)

const (
	dashboardRangeToday  = "today"
	dashboardRange7Days  = "7d"
	dashboardRange30Days = "30d"
)

type dashboardPeriod = observability.DashboardPeriod
type dashboardBusinessOverview = observability.DashboardBusiness
type dashboardServiceOverview = observability.DashboardService
type dashboardSubscriptionHealth = observability.DashboardSubscriptionHealth
type dashboardAttentionOverview = observability.DashboardAttention
type dashboardInfrastructureOverview = observability.DashboardInfrastructure
type dashboardCoverage = observability.DashboardCoverage
type dashboardTrendPoint = observability.DashboardTrendPoint

type dashboardTrendRow struct {
	BucketStart  string `gorm:"column:bucket_start"`
	RevenueCents int64  `json:"revenue_cents" gorm:"column:revenue_cents"`
	PaidOrders   int64  `json:"paid_orders" gorm:"column:paid_orders"`
	NewOrders    int64  `json:"new_orders" gorm:"column:new_orders"`
	RenewOrders  int64  `json:"renew_orders" gorm:"column:renew_orders"`
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

	response, err := h.loadDashboardOverview(r.Context(), period, now, time.UTC)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, response)
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

func (h *handlers) loadDashboardOverview(ctx context.Context, period dashboardPeriod, now time.Time, location *time.Location) (dashboardOverviewResponse, error) {
	calendarBuckets := dashboardCalendarBuckets(period, location)
	buckets := make([]observability.DashboardTrendBucket, 0, len(calendarBuckets))
	for _, bucket := range calendarBuckets {
		buckets = append(buckets, observability.DashboardTrendBucket{Key: bucket.Key, StartUTC: bucket.StartUTC, EndUTC: bucket.EndUTC})
	}
	snapshot, err := h.services.Dashboard.Load(ctx, period, now, buckets)
	if err != nil {
		return dashboardOverviewResponse{}, err
	}
	return dashboardOverviewResponse{Period: period, Business: snapshot.Business, Service: snapshot.Service, Subscriptions: snapshot.Subscriptions, Attention: snapshot.Attention, Infrastructure: snapshot.Infrastructure, Coverage: snapshot.Coverage, Trend: snapshot.Trend, AsOf: now}, nil
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
