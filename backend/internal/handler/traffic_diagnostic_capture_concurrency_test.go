package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMySQLDiagnosticCollectionLastSlotUsesCurrentReadAcrossIndependentReports(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	h, c := accountingFixtureFromOrder(t, f)
	enableDiagnosticCollectionFixture(t, h)
	node2 := model.Node{Name: "capture-second-node", Region: "test", Config: "{}", AssetTag: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")}
	if err := h.db.Create(&node2).Error; err != nil {
		t.Fatal(err)
	}
	endpoint2 := model.ProtocolEndpoint{NodeID: node2.ID, Name: "capture-second-endpoint", RuntimeKey: uuid.NewString(), Protocol: "vless", Address: "127.0.0.1", Port: 443, PublicPort: 443, IsActive: true, MultiplierMilli: 1000, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := h.db.Create(&endpoint2).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: f.group.ID, ProtocolEndpointID: endpoint2.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{c.NodeID, endpoint2.NodeID} {
		if err := h.db.Model(&model.Node{}).Where("id = ?", id).Update("traffic_secret", "capture-race-key").Error; err != nil {
			t.Fatal(err)
		}
	}
	other := model.User{Email: "capture-other@example.test", Password: "unused", Status: userStatusActive}
	if err := h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: other.ID, Email: other.Email})
	if err != nil {
		t.Fatal(err)
	}
	otherFixture := f
	otherFixture.token = token
	paid := f.paid(t, otherFixture.create(t, 0).ID) // original administrator confirms another owner's actual order
	if paid.UserID != other.ID {
		t.Fatal("race did not create independent paid owner")
	}
	makeInput := func(id string, user, node, endpoint uint) metering.AuthenticatedNodeReport {
		return metering.AuthenticatedNodeReport{UserID: user, NodeID: node, ProtocolEndpointID: endpoint, ExpectedCredential: "capture-race-key", Timestamp: time.Now().UTC(), ReportID: id, Nonce: id, DownloadBytes: 3, Meta: id}
	}
	for i := 0; i < 127; i++ {
		if _, err := h.services.NodeReports().Record(t.Context(), makeInput(fmt.Sprintf("capture-warm-%d", i), c.UserID, c.NodeID, c.ProtocolEndpointID)); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := h.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(2) // this isolated test only
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	entered := make(chan string, 2)
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	var onceA, onceB sync.Once
	var workers sync.WaitGroup
	const callback = "test:capture_last_slot_barrier"
	if err := h.db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "traffic_records" {
			return
		}
		record, ok := tx.Statement.Dest.(*model.TrafficRecord)
		if !ok {
			return
		}
		var gate <-chan struct{}
		switch record.ReportID {
		case "capture-last-a":
			gate = releaseA
		case "capture-last-b":
			gate = releaseB
		default:
			return
		}
		// Both transactions have performed their ordinary ownership reads and
		// inserted separate financial facts before either capture can commit.
		entered <- record.ReportID
		select {
		case <-gate:
		case <-ctx.Done():
			tx.AddError(ctx.Err())
		}
	}); err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() {
		cancel()
		onceA.Do(func() { close(releaseA) })
		onceB.Do(func() { close(releaseB) })
		workers.Wait()
		_ = h.db.Callback().Create().Remove(callback)
	}()
	type result struct {
		index      int
		connection uint64
		out        metering.NodeReportResult
		err        error
	}
	results := make(chan result, 2)
	connections := make(chan uint64, 2)
	inputs := []metering.AuthenticatedNodeReport{makeInput("capture-last-a", c.UserID, c.NodeID, c.ProtocolEndpointID), makeInput("capture-last-b", other.ID, endpoint2.NodeID, endpoint2.ID)}
	for i, input := range inputs {
		workers.Add(1)
		go func(index int, in metering.AuthenticatedNodeReport) {
			defer workers.Done()
			r := result{index: index}
			r.err = h.db.WithContext(ctx).Connection(func(connection *gorm.DB) error {
				if err := connection.Raw("SELECT CONNECTION_ID()").Scan(&r.connection).Error; err != nil {
					return err
				}
				connections <- r.connection
				return connection.Transaction(func(tx *gorm.DB) error {
					var initial int64
					if err := tx.Model(&model.TrafficDiagnosticMaterial{}).Where("retired_at IS NULL").Count(&initial).Error; err != nil {
						return err
					}
					if initial != 127 {
						return fmt.Errorf("initial repeatable-read capture view = %d, want127", initial)
					}
					service := metering.NodeReports{Repository: meteringstore.NodeReports{DB: tx, Expire: entitlementstore.ExpireInTransaction, QuotaEvent: entitlementstore.RecordQuotaEvent}}
					var err error
					r.out, err = service.Record(ctx, in)
					return err
				}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
			})
			results <- r
		}(i, input)
	}
	ids := map[uint64]bool{}
	for range 2 {
		select {
		case id := <-connections:
			ids[id] = true
		case <-ctx.Done():
			t.Fatal("independent connections unavailable", ctx.Err())
		}
	}
	if len(ids) != 2 || ids[0] {
		t.Fatal("capture contenders shared a physical connection")
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-entered:
			seen[id] = true
		case r := <-results:
			t.Fatal("report exited before both authority snapshots", r.index, r.err)
		case <-ctx.Done():
			t.Fatal("independent authority transactions serialized before admission", ctx.Err())
		}
	}
	if len(seen) != 2 {
		t.Fatal("duplicate transaction barrier")
	}
	onceA.Do(func() { close(releaseA) })
	var first result
	select {
	case first = <-results:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if first.err != nil || first.index != 0 || first.out.Record.Meta != inputs[0].Meta {
		t.Fatal("first last-slot admission failed", first.err)
	}
	onceB.Do(func() { close(releaseB) })
	var second result
	select {
	case second = <-results:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var receipt struct{ Type, Reason string }
	if second.err != nil || second.index != 1 || json.Unmarshal([]byte(second.out.Record.Meta), &receipt) != nil || receipt.Reason != "collection_capacity" {
		t.Fatal("old transaction snapshot over-admitted final slot", second.err, receipt)
	}
	var pending int64
	if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Where("retired_at IS NULL").Count(&pending).Error; err != nil || pending != 128 {
		t.Fatal("global capture cap exceeded", pending, err)
	}
	var headers int64
	if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Where("traffic_record_id = ?", second.out.Record.ID).Count(&headers).Error; err != nil || headers != 0 {
		t.Fatal("omitted contender was classified", headers, err)
	}
	var source model.Subscription
	if err := h.db.First(&source, paid.SubscriptionID).Error; err != nil || source.FlowUsed != 3 {
		t.Fatal("capacity loser lost actual accounting", source.FlowUsed, err)
	}
	for i, input := range inputs {
		out, err := h.services.NodeReports().Record(t.Context(), input)
		if err != nil || !out.Duplicate || out.Record.ID != []uint{first.out.Record.ID, second.out.Record.ID}[i] {
			t.Fatal("capture contention replay recharged", err)
		}
	}
}
