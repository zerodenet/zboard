package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const pendingTrendPath = "/api/v1/traffic/trends?from=2026-09-01&to=2026-09-01"

func pendingTrendFixture(t *testing.T) trafficReadFixture {
	t.Helper()
	f := newTrafficReadFixture(t)
	if err := f.h.db.Create(&model.SystemConfig{ConfigKey: systemTimezoneConfigKey, Value: "UTC"}).Error; err != nil {
		t.Fatal(err)
	}
	sub := model.Subscription{ID: 1, UserID: 1, FlowTotal: 1000, FlowUsed: 30, Status: "active", EndAt: time.Now().UTC().Add(24 * time.Hour)}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	appendPendingTrendFact(t, f.h.db, "initial-pending", 30)
	return f
}
func appendPendingTrendFact(t *testing.T, db *gorm.DB, identity string, used int64) {
	t.Helper()
	record := model.TrafficRecord{UserID: 1, SubscriptionID: 1, NodeID: 1, ReportID: identity, Nonce: identity, RawBytes: used, DownloadBytes: used, UsedBytes: used, ProtocolMultiplierMilli: 1000, At: time.Date(2026, 9, 1, 1, 15, 0, 0, time.UTC)}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return meteringstore.RecordUsageProjections(tx, []model.TrafficRecord{record})
	}); err != nil {
		t.Fatal(err)
	}
}
func pendingTrendResponse(t *testing.T, h http.HandlerFunc, path, token string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
	t.Helper()
	response := httptest.NewRecorder()
	h(response, announcementRequest(http.MethodGet, path, token, ""))
	var envelope struct {
		Data  map[string]json.RawMessage `json:"data"`
		Error json.RawMessage            `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code == http.StatusOK && len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		t.Fatalf("successful state has API error: %s", response.Body.String())
	}
	return response, envelope.Data
}
func assertPendingTrendWire(t *testing.T, response *httptest.ResponseRecorder, data map[string]json.RawMessage, user uint, admin bool, timezone string) {
	t.Helper()
	if response.Code != http.StatusOK || response.Header().Get("X-Zboard-Statistics-State") != "pending" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("pending state became failure or cacheable snapshot: %d %s %v", response.Code, response.Body.String(), response.Header())
	}
	if string(data["projection_pending"]) != "true" || string(data["from"]) != `"2026-09-01"` || string(data["to"]) != `"2026-09-01"` {
		t.Fatal("pending lost range/state", response.Body.String())
	}
	for _, field := range []string{"points", "record_count", "connection_sample_count", "peak_connections", "truncated", "subscriptions", "as_of"} {
		if string(data[field]) != "null" {
			t.Fatalf("pending %s pretended to have a measurement: %s", field, data[field])
		}
	}
	var scope metering.TrafficTrendScope
	if err := json.Unmarshal(data["scope"], &scope); err != nil {
		t.Fatal(err)
	}
	if scope.Administrative != admin || scope.UserID != user || scope.TimeZone != timezone {
		t.Fatalf("pending lost authorized calendar scope: %+v", scope)
	}
}

func TestTrafficTrendPendingIsUnknownOnAllRoutesAndPreservesQuota(t *testing.T) {
	f := pendingTrendFixture(t)
	routes := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"UTC", f.h.TrafficTrendsHandler}, {"calendar", f.h.TrafficTrendsSystemCalendarHandler},
		{"UTC-principal", f.h.TrafficTrendsWithPrincipalFlowsHandler}, {"calendar-principal", f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler},
	}
	for _, route := range routes {
		for _, admin := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/admin=%t", route.name, admin), func(t *testing.T) {
				path, token := pendingTrendPath+"&user_id=2&subscription_id=1", f.token
				if admin {
					path, token = "/api/v1/admin/traffic/trends?from=2026-09-01&to=2026-09-01&user_id=1&subscription_id=1", f.admin
				}
				f.capture()
				response, data := pendingTrendResponse(t, route.handler, path, token)
				assertPendingTrendWire(t, response, data, 1, admin, "UTC")
				var scope metering.TrafficTrendScope
				_ = json.Unmarshal(data["scope"], &scope)
				if scope.SubscriptionID != 1 {
					t.Fatal("lost subscription filter")
				}
				for _, query := range f.log.queries {
					if strings.Contains(query, "principal_flow_scope_observations") || strings.Contains(query, "FROM `traffic_records`") || strings.Contains(query, "FROM `traffic_usage_hourly`") {
						t.Fatal("pending performed principal/history/complete aggregation", query)
					}
				}
			})
		}
	}
	var pending int64
	if err := f.h.db.Table("metering_usage_projection_pending").Count(&pending).Error; err != nil || pending != 1 {
		t.Fatal("HTTP drained durable projection work", pending, err)
	}
	quota, err := f.h.services.UsageQuota().Read(t.Context(), 1, metering.UsageSummaryQuery{})
	if err != nil || quota.RemainingBytes != 970 {
		t.Fatal("pending chart blocked or changed quota", quota, err)
	}
	quotaResponse, quotaData := pendingTrendResponse(t, f.h.TrafficSummaryHandler, "/api/v1/traffic/summary?view=quota", f.token)
	if quotaResponse.Code != http.StatusOK || string(quotaData["remaining_bytes"]) != "970" {
		t.Fatal("pending trend blocked HTTP core quota", quotaResponse.Code, quotaResponse.Body.String())
	}
	globalResponse, globalData := pendingTrendResponse(t, f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler, "/api/v1/admin/traffic/trends?from=2026-09-01&to=2026-09-01&node_id=1", f.admin)
	assertPendingTrendWire(t, globalResponse, globalData, 0, true, "UTC")
	var globalScope metering.TrafficTrendScope
	_ = json.Unmarshal(globalData["scope"], &globalScope)
	if globalScope.NodeID != 1 {
		t.Fatal("pending wrapper lost global/node-filter scope", globalScope)
	}
	if err := f.h.db.Model(&model.SystemConfig{}).Where("config_key = ?", systemTimezoneConfigKey).Update("value", "Asia/Kathmandu").Error; err != nil {
		t.Fatal(err)
	}
	response, data := pendingTrendResponse(t, f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler, pendingTrendPath, f.token)
	assertPendingTrendWire(t, response, data, 1, false, "Asia/Kathmandu")
	drainUsageProjections(t, f.h.db)
	for _, route := range routes {
		complete, values := pendingTrendResponse(t, route.handler, pendingTrendPath, f.token)
		var scope metering.TrafficTrendScope
		if err := json.Unmarshal(values["scope"], &scope); err != nil {
			t.Fatal(err)
		}
		timezone := "UTC"
		if strings.HasPrefix(route.name, "calendar") {
			timezone = "Asia/Kathmandu"
		}
		if complete.Code != http.StatusOK || complete.Header().Get("X-Zboard-Statistics-State") != "complete" || string(values["projection_pending"]) != "false" || string(values["record_count"]) != "1" || string(values["as_of"]) == "null" || scope.UserID != 1 || scope.TimeZone != timezone {
			t.Fatalf("%s did not retain complete snapshot/header/scope: %d %s", route.name, complete.Code, complete.Body.String())
		}
	}
}

func TestTrafficTrendPendingBypassesCompleteCacheAndRecoversExactFacts(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	var before trafficTrendResponse
	f.get(t, pendingTrendPath, false, f.h.TrafficTrendsHandler, &before)
	if before.RecordCount != 6 || before.Points[0].UsedBytes != 210 {
		t.Fatal("initial complete facts changed", before)
	}
	f.h.trafficTrendsCache.mu.Lock()
	for _, entry := range f.h.trafficTrendsCache.entries {
		entry.expires = time.Now().Add(time.Minute)
	}
	f.h.trafficTrendsCache.mu.Unlock()
	appendPendingTrendFact(t, f.h.db, "after-complete-snapshot", 23)
	response, data := pendingTrendResponse(t, f.h.TrafficTrendsHandler, pendingTrendPath, f.token)
	assertPendingTrendWire(t, response, data, 1, false, "UTC")
	other, otherData := pendingTrendResponse(t, f.h.TrafficTrendsHandler, "/api/v1/admin/traffic/trends?user_id=2&from=2026-09-01&to=2026-09-01", f.admin)
	if other.Code != http.StatusOK || other.Header().Get("X-Zboard-Statistics-State") != "complete" || string(otherData["projection_pending"]) != "false" {
		t.Fatal("unrelated pending blocked other scope", other.Code, other.Body.String())
	}
	// Projection runs only in the separately invoked worker, never in the request.
	drainUsageProjections(t, f.h.db)
	f.h.trafficTrendsCache.mu.Lock()
	for _, entry := range f.h.trafficTrendsCache.entries {
		entry.expires = time.Time{}
	}
	f.h.trafficTrendsCache.mu.Unlock()
	complete, completeData := pendingTrendResponse(t, f.h.TrafficTrendsHandler, pendingTrendPath, f.token)
	var recovered trafficTrendResponse
	encoded, _ := json.Marshal(completeData)
	if err := json.Unmarshal(encoded, &recovered); err != nil {
		t.Fatal(err)
	}
	if complete.Code != http.StatusOK || complete.Header().Get("X-Zboard-Statistics-State") != "complete" || string(completeData["projection_pending"]) != "false" || recovered.RecordCount != 7 || recovered.Points[0].UsedBytes != 233 || recovered.AsOf.IsZero() || recovered.Scope.UserID != 1 || recovered.Scope.TimeZone != "UTC" {
		t.Fatalf("recovered exact measured facts changed: %d %+v", complete.Code, recovered)
	}
}

func TestTrafficTrendPendingNeverMasksDatabaseOrPrincipalFailure(t *testing.T) {
	for _, table := range []string{"metering_usage_projection_pending", "traffic_usage_hourly", "principal_flow_scope_observations"} {
		t.Run(table, func(t *testing.T) {
			f := pendingTrendFixture(t)
			if table != "metering_usage_projection_pending" {
				drainUsageProjections(t, f.h.db)
			}
			if err := f.h.db.Exec("DROP TABLE " + table).Error; err != nil {
				t.Fatal(err)
			}
			handler := f.h.TrafficTrendsHandler
			if table == "principal_flow_scope_observations" {
				handler = f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler
			}
			response, _ := pendingTrendResponse(t, handler, pendingTrendPath, f.token)
			if response.Code < 500 || response.Header().Get("X-Zboard-Statistics-State") != "" || strings.Contains(response.Body.String(), `"projection_pending":true`) {
				t.Fatalf("real failure disguised as pending/completeness: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestTrafficTrendPendingPreservesQueryAndPermissionFailures(t *testing.T) {
	f := pendingTrendFixture(t)
	for _, query := range []string{"from=bad", "from=2024-01-01&to=2026-09-01", "from=2026-09-01&to=2026-09-01&node_id=-1", "from=2026-09-01&to=2026-09-01&subscription_id=invalid"} {
		response, _ := pendingTrendResponse(t, f.h.TrafficTrendsHandler, "/api/v1/traffic/trends?"+query, f.token)
		if response.Code != http.StatusBadRequest {
			t.Fatal("pending bypassed query guard", query, response.Code, response.Body.String())
		}
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 99).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	response, _ := pendingTrendResponse(t, f.h.TrafficTrendsHandler, "/api/v1/admin/traffic/trends?from=2026-09-01&to=2026-09-01", f.admin)
	if response.Code != http.StatusForbidden && response.Code != http.StatusUnauthorized {
		t.Fatal("pending bypassed revocation", response.Code, response.Body.String())
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := f.h.services.TrafficTrends(nil).Read(canceled, 1, metering.TrafficTrendQuery{From: from, Days: 1, Timezone: "UTC", Buckets: []metering.TrendBucket{{Key: "2026-09-01", StartUTC: from, EndUTC: from.Add(24 * time.Hour)}}})
	if !errors.Is(err, context.Canceled) || errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("cancellation disguised as asynchronous pending", err)
	}
}

func TestTrafficTrendPendingSerializationDiscardsEverySuppliedSnapshotValue(t *testing.T) {
	peak := int64(100)
	response := trafficTrendResponse{ProjectionPending: true, From: "2026-09-01", To: "2026-09-01", RecordCount: 99, ConnectionSampleCount: 100, PeakConnections: &peak, Truncated: true, AsOf: time.Now().UTC(), Points: []trafficTrendPoint{{Date: "2026-09-01", UsedBytes: 99}}}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"points", "record_count", "connection_sample_count", "peak_connections", "truncated", "subscriptions", "as_of"} {
		if string(data[field]) != "null" {
			t.Fatal("pending serializer leaked complete/zero value", field, string(data[field]))
		}
	}
}
