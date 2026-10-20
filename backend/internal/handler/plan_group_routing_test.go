package handler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestPlanGroupNodeWidePublicationRetainsEveryForwardRoute(t *testing.T) {
	assertPlanGroupNodeWideRoutes(t, newOrderFixture(t))
}
func TestMySQLPlanGroupNodeWidePublicationRetainsEveryForwardRoute(t *testing.T) {
	assertPlanGroupNodeWideRoutes(t, newMySQLOrderFixture(t))
}
func assertPlanGroupNodeWideRoutes(t *testing.T, base orderFixture) {
	t.Helper()
	db := base.h.db
	endpoint := planGroupEndpoint(t, db, "node-wide-route")
	second := endpoint
	second.ID, second.Name, second.RuntimeKey, second.Port = 0, "second-route", uuid.NewString(), 9443
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	var nodes []uint
	for i, target := range []model.ProtocolEndpoint{endpoint, second, second} {
		front := model.Node{Name: fmt.Sprintf("front-%d", i), Address: "front.example.test", Config: "{}"}
		if err := db.Create(&front).Error; err != nil {
			t.Fatal(err)
		}
		entry := model.NetworkEntry{EndpointID: target.ID, NodeID: front.ID, Name: fmt.Sprintf("front-route-%d", i), Network: "tcp", Address: "front.example.test", Port: 7000 + i, PublicPort: 7000 + i, Enabled: true}
		if err := db.Create(&entry).Error; err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, front.ID)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return networkstore.EnqueueNodePublication(tx, endpoint.NodeID, 0, 1) }); err != nil {
		t.Fatal(err)
	}
	nodes = append(nodes, endpoint.NodeID)
	for _, node := range nodes {
		var queue model.NodeConfigPublish
		if err := db.First(&queue, node).Error; err != nil || queue.Generation != 1 || queue.EndpointID != 0 {
			t.Fatalf("node-wide publication lost route %d: %+v %v", node, queue, err)
		}
	}
}

// An endpoint placement edit reaches a real node-parent wait while group scope
// removal retains its credential/parent locks. Complete revocation and routing
// before releasing those locks; the waiting edit must not revive removed scope.
func TestMySQLPlanGroupRemovalRacesEndpointMutationWithoutReverseLocks(t *testing.T) {
	base := newMySQLOrderFixture(t)
	f := newPlanGroupFixture(t, base)
	admin := planQuotaRaceAdmin(t, f.fullReplacementFixture)
	service := network.ProtocolEndpointMutations{Repository: networkstore.ProtocolEndpointMutations{DB: f.db}, Cipher: base.h.credentialCipher, NewRuntimeKey: uuid.NewString}
	before, err := service.Load(t.Context(), admin, f.old.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := planGroupEndpointEdit(before.Endpoint)
	request.Port, request.PublicPort = 8443, 8443
	type revocationMarker struct{}
	held, release := make(chan struct{}), make(chan struct{})
	var entered, unblock sync.Once
	defer unblock.Do(func() { close(release) })
	const hook = "test:group-removal-before-endpoint-edit"
	if err := f.db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
		if q.Error == nil && q.RowsAffected > 0 && q.Statement.Context.Value(revocationMarker{}) == true && q.Statement.Table == "protocol_credentials" {
			entered.Do(func() { close(held); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(hook)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	groupDone := make(chan error, 1)
	go func() {
		_, err := base.h.services.PlanUpdate.Update(context.WithValue(ctx, revocationMarker{}, true), admin, base.planRecord.ID, commerce.PlanUpdateRequest{NodeGroupID: &f.group.ID, ExpectedRevision: &base.planRecord.Revision})
		groupDone <- err
	}()
	select {
	case <-held:
	case err := <-groupDone:
		t.Fatalf("scope removal did not hold credential lock: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	editDone := make(chan error, 1)
	go func() { _, err := service.Save(ctx, admin, &before, request); editDone <- err }()
	planGroupAwaitRowLock(t, ctx, f.db, "nodes", editDone)
	unblock.Do(func() { close(release) })
	groupErr := <-groupDone
	if err := <-editDone; err != nil {
		t.Fatalf("endpoint mutation failed under scope removal: %v", err)
	}
	if groupErr != nil {
		t.Fatalf("scope removal failed under waiting endpoint edit: %v", groupErr)
	}

	var credential model.ProtocolCredential
	if err := f.db.First(&credential, f.oldCredential.ID).Error; err != nil || credential.Status != "revoked" || credential.RevokedAt == nil || credential.ListenPort != 8443 {
		t.Fatalf("placement mutation lost revocation: %+v %v", credential, err)
	}
	for _, node := range []uint{f.old.NodeID, f.shared.NodeID, f.added.NodeID} {
		var queue model.NodeConfigPublish
		if err := f.db.First(&queue, node).Error; err != nil {
			t.Fatal("concurrent scope/placement change lost durable publication", err)
		}
	}
}

func planGroupEndpointEdit(endpoint network.ProtocolEndpointRecord) network.ProtocolEndpointMutationRequest {
	// The old minimal outbox fixture has no public address. A real editor
	// request must provide one before it can exercise placement concurrency.
	if endpoint.Address == "" {
		endpoint.Address = "edited.example.test"
	}
	active := endpoint.IsActive
	return network.ProtocolEndpointMutationRequest{ID: endpoint.ID, NodeID: endpoint.NodeID, Name: endpoint.Name, Protocol: endpoint.Protocol, Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort, Cipher: endpoint.Cipher, ParentProtocolID: endpoint.ParentProtocolID, MultiplierMilli: endpoint.MultiplierMilli, IsActive: &active, ServerConfig: endpoint.ServerConfig, ClientConfig: endpoint.ClientConfig, OptionalConfig: endpoint.OptionalConfig, Tags: endpoint.Tags, CredentialProtocols: []string{"vless"}}
}

func planGroupAwaitRowLock(t *testing.T, ctx context.Context, db *gorm.DB, table string, done <-chan error) {
	t.Helper()
	for ctx.Err() == nil {
		var waits int64
		if err := db.WithContext(ctx).Raw("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=DATABASE() AND l.OBJECT_NAME=?", table).Scan(&waits).Error; err != nil {
			t.Fatal(err)
		}
		if waits > 0 {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("endpoint mutation did not wait for %s row lock: %v", table, err)
		case <-time.After(time.Millisecond):
		}
	}
	t.Fatal("real row-lock wait was not established", table, ctx.Err())
}

// A replacement closes its old credential epoch before issuing the next one.
// A shared endpoint placement edit must serialize with that transition without
// a credential -> endpoint / endpoint -> credential lock cycle.
func TestMySQLPlanGroupSharedEndpointMutationRacesReplacementPayment(t *testing.T) {
	assertPlanGroupSharedEndpointPayment(t, false)
}
func TestMySQLPlanGroupSharedEndpointMutationRacesRecoveryPayment(t *testing.T) {
	assertPlanGroupSharedEndpointPayment(t, true)
}
func assertPlanGroupSharedEndpointPayment(t *testing.T, recovering bool) {
	base := newMySQLOrderFixture(t)
	f := newPlanGroupFixture(t, base)
	admin := planQuotaRaceAdmin(t, f.fullReplacementFixture)
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "replacement-vs-endpoint-placement"}
	if recovering {
		var sub model.Subscription
		if err := f.db.First(&sub, f.subscription).Error; err != nil {
			t.Fatal(err)
		}
		*f.at = sub.EndAt.Add(24 * time.Hour)
		request.PlanSKUID = base.skuRecord.ID
		request.IdempotencyKey = "recovery-vs-endpoint-placement"
	}
	quote, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	pending, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	service := network.ProtocolEndpointMutations{Repository: networkstore.ProtocolEndpointMutations{DB: f.db}, Cipher: base.h.credentialCipher, NewRuntimeKey: uuid.NewString}
	before, err := service.Load(t.Context(), admin, f.old.ID)
	if err != nil {
		t.Fatal(err)
	}
	edit := planGroupEndpointEdit(before.Endpoint)
	edit.Port, edit.PublicPort = 8443, 8443
	type replacementMarker struct{}
	held, release := make(chan struct{}), make(chan struct{})
	var entered, unblock sync.Once
	defer unblock.Do(func() { close(release) })
	const hook = "test:replacement-before-endpoint-edit"
	if err := f.db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
		if q.Error == nil && q.RowsAffected > 0 && q.Statement.Context.Value(replacementMarker{}) == true && q.Statement.Table == "protocol_credentials" {
			entered.Do(func() { close(held); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(hook)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	payDone := make(chan error, 1)
	go func() {
		_, err := f.payments.Pay(context.WithValue(ctx, replacementMarker{}, true), 1, pending.ID)
		payDone <- err
	}()
	select {
	case <-held:
	case err := <-payDone:
		t.Fatalf("payment did not hold its closing credential: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	editDone := make(chan error, 1)
	go func() { _, err := service.Save(ctx, admin, &before, edit); editDone <- err }()
	planGroupAwaitRowLock(t, ctx, f.db, "nodes", editDone)
	unblock.Do(func() { close(release) })
	payErr, editErr := <-payDone, <-editDone
	if payErr != nil || editErr != nil {
		t.Fatalf("replacement and endpoint placement did not serialize: payment=%v endpoint=%v", payErr, editErr)
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 1 {
		t.Fatal("replacement epoch not committed exactly once", err)
	}
	var credential model.ProtocolCredential
	if err := f.db.Where("subscription_id = ? AND protocol_endpoint_id = ? AND current_scope = ?", sub.ID, f.old.ID, 1).First(&credential).Error; err != nil || credential.EntitlementEpoch != 1 || credential.ListenPort != 8443 || !credential.DeliveryRequired {
		t.Fatalf("new epoch lost current placement: %+v %v", credential, err)
	}
	var closed model.ProtocolCredential
	if err := f.db.First(&closed, f.oldCredential.ID).Error; err != nil || closed.Status != "revoked" || closed.CurrentScope != nil || closed.Secret != "" {
		t.Fatalf("closed credential revived: %+v %v", closed, err)
	}
	var order model.Order
	if err := f.db.First(&order, pending.ID).Error; err != nil || order.Status != "paid" {
		t.Fatal("serialized replacement was not paid", err)
	}
}
