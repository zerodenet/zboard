package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPublicationQueuePreservesNewerGenerationAndLeaseOwner(t *testing.T) {
	db, other := administrationFixture(t)
	if err := db.Create(&model.Node{ID: 1, Name: "publish-test", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePublication(db, 1, 11, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond).Add(time.Second)
	queue := PublicationQueue{DB: db}
	old, ok, err := queue.Claim(context.Background(), now)
	if err != nil || !ok {
		t.Fatalf("claim old: ok=%t err=%v", ok, err)
	}
	if err := EnqueuePublication(other, 1, 12, 0); err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete(context.Background(), old, now, nil); err != nil {
		t.Fatal(err)
	}
	current, ok, err := (PublicationQueue{DB: other}).Claim(context.Background(), now)
	if err != nil || !ok {
		var state model.NodeConfigPublish
		_ = other.First(&state, "node_id = ?", 1).Error
		t.Fatalf("claim current: ok=%t err=%v state=%+v", ok, err, state)
	}
	if current.Generation != old.Generation+1 || current.EndpointID != 12 || current.LeaseToken == old.LeaseToken {
		t.Fatalf("new generation was not preserved: old=%+v current=%+v", old, current)
	}
	if renewed, err := queue.Renew(context.Background(), old, now.Add(network.PublicationLease)); err != nil || renewed {
		t.Fatalf("stale lease renewed: renewed=%t err=%v", renewed, err)
	}
}

func TestPublicationQueuePersistsFailureAndBackoff(t *testing.T) {
	db, _ := administrationFixture(t)
	if err := db.Create(&model.Node{ID: 1, Name: "publish-test", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePublication(db, 1, 11, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond).Add(time.Second)
	queue := PublicationQueue{DB: db}
	item, ok, err := queue.Claim(context.Background(), now)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%t err=%v", ok, err)
	}
	failure := errors.New("node offline")
	if err := queue.Complete(context.Background(), item, now, failure); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := queue.Claim(context.Background(), now.Add(4*time.Second)); err != nil || ok {
		t.Fatalf("backoff ignored: ok=%t err=%v", ok, err)
	}
	retry, ok, err := queue.Claim(context.Background(), now.Add(5*time.Second))
	if err != nil || !ok || retry.Attempts != 1 || retry.LastError != failure.Error() {
		var state model.NodeConfigPublish
		_ = db.First(&state, "node_id = ?", 1).Error
		t.Fatalf("retry state: item=%+v ok=%t err=%v state=%+v", retry, ok, err, state)
	}
}
