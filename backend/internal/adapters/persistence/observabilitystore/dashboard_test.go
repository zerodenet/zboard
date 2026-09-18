package observabilitystore

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type dashboardQueryCounter struct {
	logger.Interface
	queries atomic.Int64
}

func (l *dashboardQueryCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.queries.Add(1)
	l.Interface.Trace(ctx, begin, fc, err)
}

func TestDashboardProjectionPreservesFactsInFourQueries(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "dashboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	paidAt, previousPaidAt := now.Add(-time.Hour), now.Add(-25*time.Hour)
	orders := []model.Order{
		{TradeNo: "current", Status: "paid", PaidAt: &paidAt, PaidAmount: 120, Currency: "cny", OrderType: "new"},
		{TradeNo: "previous", Status: "paid", PaidAt: &previousPaidAt, PaidAmount: 70, Currency: "USD", OrderType: "renew"},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}
	subscriptions := []model.Subscription{
		{Status: "active", EndAt: now.Add(12 * time.Hour), FlowTotal: 100, FlowUsed: 10, CreatedAt: now.Add(-time.Hour)},
		{Status: "active", EndAt: now.Add(10 * 24 * time.Hour), FlowTotal: 100, FlowUsed: 100, CreatedAt: now.Add(-25 * time.Hour)},
	}
	if err := db.Create(&subscriptions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TrafficRecord{UsedBytes: 321, At: paidAt}).Error; err != nil {
		t.Fatal(err)
	}
	onlineAt := now.Add(-time.Minute)
	nodes := []model.Node{{Name: "online", IsEnabled: true, ConnectorLastSeenAt: &onlineAt, SSHHostKeyFingerprint: "sha256:key", SSHVerifiedAt: &onlineAt, TrafficSecretPrefix: "secret"}, {Name: "offline", IsEnabled: true}}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{Email: "dashboard@example.test", Password: "unused", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "group", Code: "dashboard", IsEnabled: true}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&meteringstore.PrincipalFlowScopeCurrent{ScopeType: meteringstore.ScopeSubscription, ScopeID: subscriptions[0].ID, ActiveFlows: 3, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProtocolEndpoint{NodeID: nodes[0].ID, Protocol: "vless", Name: "endpoint", IsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Plan{Name: "plan", Slug: "plan", NodeGroupID: group.ID, IsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Ticket{TicketNo: "ticket", UserID: user.ID, Subject: "help", Category: "general", Status: "open", LastMessageAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProtocolDeployment{ProtocolEndpointID: 1, NodeID: nodes[0].ID, Status: "failed"}).Error; err != nil {
		t.Fatal(err)
	}

	counter := &dashboardQueryCounter{Interface: db.Config.Logger}
	counted := db.Session(&gorm.Session{Logger: counter})
	period := observability.DashboardPeriod{Range: "today", From: now.Add(-2 * time.Hour), To: now, PreviousFrom: now.Add(-26 * time.Hour), PreviousTo: now.Add(-24 * time.Hour), Bucket: "hour", Timezone: "UTC"}
	buckets := []observability.DashboardTrendBucket{{Key: "b000", StartUTC: period.From, EndUTC: now.Add(-time.Hour)}, {Key: "b001", StartUTC: now.Add(-time.Hour), EndUTC: now}}
	result, err := (observability.Dashboard{Repository: Dashboard{DB: counted}}).Load(context.Background(), period, now, buckets)
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.queries.Load(); got != 4 {
		t.Fatalf("dashboard used %d queries, want 4", got)
	}
	if result.Business.RevenueCents != 120 || result.Business.PreviousRevenueCents != 70 || result.Business.PaidOrders != 1 || !result.Business.MixedCurrency {
		t.Fatalf("business = %+v", result.Business)
	}
	if result.Business.ActiveSubscriptions != 1 || result.Subscriptions.ExpiringWithin24Hours != 1 || result.Subscriptions.QuotaExhausted != 1 {
		t.Fatalf("subscriptions = business:%+v health:%+v", result.Business, result.Subscriptions)
	}
	if result.Service.TrafficBytes != 321 || result.Service.EnabledNodes != 2 || result.Service.OnlineNodes != 1 || result.Service.ActiveSubscriptions == nil || *result.Service.ActiveSubscriptions != 1 || result.Service.ActiveFlows == nil || *result.Service.ActiveFlows != 3 {
		t.Fatalf("service = %+v", result.Service)
	}
	if result.Attention.OfflineNodes != 1 || result.Attention.PendingTickets != 1 || result.Attention.UnresolvedDeployments != 1 || result.Infrastructure.SSHVerified != 1 || result.Infrastructure.TrafficReady != 1 || result.Infrastructure.ActiveProtocolEndpoints != 1 || result.Infrastructure.PublishedPlans != 1 {
		t.Fatalf("attention/infrastructure = %+v / %+v", result.Attention, result.Infrastructure)
	}
	if len(result.Trend) != 2 || result.Trend[1].RevenueCents != 120 {
		t.Fatalf("trend = %+v", result.Trend)
	}

	counter.queries.Store(0)
	totals, err := (observability.Dashboard{Repository: Dashboard{DB: counted}}).Totals(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("dashboard totals used %d queries, want 1", got)
	}
	if totals.Users != 1 || totals.ActiveUsers != 1 || totals.Nodes != 2 || totals.Plans != 1 || totals.Orders != 2 || totals.PaidOrders != 2 || totals.RevenueCents != 190 {
		t.Fatalf("identity/commerce totals = %+v", totals)
	}
	if totals.Subscriptions != 2 || totals.ActiveSubscriptions != 1 || totals.TrafficPoolBytes != 90 || totals.OfflineNodes != 2 || totals.ConnectorOnlineNodes != 1 {
		t.Fatalf("entitlement/node totals = %+v", totals)
	}
	if totals.PendingTickets != 1 || totals.FailedDeployments != 1 || totals.ProtocolEndpoints != 1 || totals.ActiveProtocolEndpoints != 1 || totals.SSHVerifiedNodes != 1 || totals.TrafficReadyNodes != 1 {
		t.Fatalf("operational totals = %+v", totals)
	}
}
