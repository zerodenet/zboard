package handler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteDiagnosticRetirementCompetingConnections(t *testing.T) {
	assertDiagnosticRetirementConnections(t, newOrderFixture(t))
}
func TestMySQLDiagnosticRetirementCompetingConnections(t *testing.T) {
	assertDiagnosticRetirementConnections(t, newMySQLBusinessOrderFixture(t))
}

func assertDiagnosticRetirementConnections(t *testing.T, f orderFixture) {
	t.Helper()
	enableDiagnosticCollectionFixture(t, f.h)
	h, c := accountingFixtureFromOrder(t, f)
	if err := h.db.Model(&model.Node{}).Where("id = ?", c.NodeID).Update("traffic_secret", "diagnostic-connections-key").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	requests := make([]metering.AuthenticatedNodeReport, 5)
	for i := range requests {
		id := fmt.Sprintf("diagnostic-connections-%d", i)
		requests[i] = metering.AuthenticatedNodeReport{NodeID: c.NodeID, UserID: c.UserID, ProtocolEndpointID: c.ProtocolEndpointID, ExpectedCredential: "diagnostic-connections-key", Timestamp: time.Now().UTC(), Nonce: id, ReportID: id, DownloadBytes: 3, Meta: strings.Repeat("m", 65535)}
		if _, err := h.services.NodeReports().Record(t.Context(), requests[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Produce actual native rounded accounting, not a synthetic proof-shaped Meta.
	var node model.Node
	if err := h.db.First(&node, c.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	events := repeatedFlowEvents(c, 3)
	for i := range events {
		events[i].AssetTag = node.AssetTag
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	var native model.TrafficRecord
	if err := h.db.Where("event_type <> ''").First(&native).Error; err != nil || native.RawBytes != 9 || native.UsedBytes != 15 || !strings.Contains(native.Meta, "rounding_numerator") {
		t.Fatal("native rounded source", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 40, subStatusActive)
	pool, err := h.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(2) // this test's private database pool, not server settings
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	start := make(chan struct{})
	closed := false
	defer func() {
		if !closed {
			close(start)
		}
		cancel()
	}()
	ready := make(chan uint64, 2)
	type result struct {
		out metering.DiagnosticMaterialRetentionResult
		err error
	}
	finished := make(chan result, 2)
	now := time.Now().UTC().Add(8 * 24 * time.Hour)
	for range 2 {
		go func() {
			var out metering.DiagnosticMaterialRetentionResult
			err := h.db.WithContext(ctx).Connection(func(connection *gorm.DB) error {
				var id uint64
				if datastore.IsSQLite(connection) {
					var enabled int
					if err := connection.Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil {
						return err
					}
					if enabled != 1 {
						return fmt.Errorf("foreign keys disabled on a competing SQLite connection")
					}
				} else {
					if err := connection.Raw("SELECT CONNECTION_ID()").Scan(&id).Error; err != nil {
						return err
					}
				}
				ready <- id
				select {
				case <-start:
				case <-ctx.Done():
					return ctx.Err()
				}
				var err error
				out, err = (meteringstore.DiagnosticMaterialRetention{DB: connection, Now: func() time.Time { return now }}).Retire(ctx, 32, meteringstore.DiagnosticMaterialReadBudget)
				return err
			})
			finished <- result{out, err}
		}()
	}
	ids := make([]uint64, 0, 2)
	for len(ids) < 2 {
		select {
		case id := <-ready:
			ids = append(ids, id)
		case r := <-finished:
			t.Fatal("connection failed before competing transaction", r.err)
		case <-ctx.Done():
			t.Fatal("two independent database connections did not open", ctx.Err())
		}
	}
	if !datastore.IsSQLite(h.db) && (ids[0] == 0 || ids[0] == ids[1]) {
		t.Fatal("MySQL workers did not use different physical connections")
	}
	close(start)
	closed = true
	retired := 0
	for range 2 {
		select {
		case r := <-finished:
			retired += r.out.Retired
			if r.err != nil && (!datastore.IsSQLite(h.db) || !strings.Contains(strings.ToLower(r.err.Error()), "database is locked")) {
				t.Fatal("retirement contention", r.err)
			}
		case <-ctx.Done():
			t.Fatal("competing retirement did not finish", ctx.Err())
		}
	}
	worker := meteringstore.DiagnosticMaterialRetention{DB: h.db, Now: func() time.Time { return now }}
	// A caller may legitimately stop at its byte budget or a locked last item.
	// Explicit continuation must finish exactly the unretired identities.
	for range 3 {
		pending, err := worker.HasPending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !pending {
			break
		}
		out, err := worker.Retire(t.Context(), 32, meteringstore.DiagnosticMaterialReadBudget)
		if err != nil {
			t.Fatal(err)
		}
		retired += out.Retired
	}
	if retired != 5 {
		t.Fatal("duplicate or missing retirement", retired)
	}
	var count int64
	if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Where("retired_at IS NOT NULL").Count(&count).Error; err != nil || count != 5 {
		t.Fatal("original source receipts missing", count, err)
	}
	var after model.TrafficRecord
	if err := h.db.First(&after, native.ID).Error; err != nil || !reflect.DeepEqual(native, after) {
		t.Fatal("native financial rounding proof was retired", err)
	}
	for _, input := range requests {
		if out, err := h.services.NodeReports().Record(t.Context(), input); err != nil || !out.Duplicate || !strings.Contains(out.Record.Meta, "diagnostic_meta_retired") {
			t.Fatal("retired-body replay", err)
		}
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal("native replay after diagnostic retirement", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 40, subStatusActive)
}
