package networkstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRecoveryClosesOnlyOlderIntentsWithoutConfirmingTheirCredentials(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	old := now.Add(-24 * time.Hour)
	rows := []model.ProtocolDeployment{
		{NodeID: node.ID, Status: "running", DesiredConfigSHA256: strings.Repeat("a", 64), StartedAt: &old, Error: "original SSH timeout"},
		{NodeID: node.ID, Status: "reconciling", DesiredConfigSHA256: strings.Repeat("b", 64), StartedAt: &old, Error: "lost acknowledgement"},
		{NodeID: node.ID, Status: "succeeded", DesiredConfigSHA256: strings.Repeat("c", 64), AppliedConfigSHA256: strings.Repeat("c", 64), StartedAt: &old},
	}
	for i := range rows {
		if err := store.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	emptyDraft := model.ProtocolDeployment{NodeID: node.ID, Status: "running", StartedAt: &old}
	if err := store.DB.Create(&emptyDraft).Error; err != nil {
		t.Fatal(err)
	}
	request := network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}
	start, err := store.BeginConfigurationPublication(ctx, request, now)
	if err != nil || start.Deployment.ID != rows[1].ID || start.LaterCompleted == nil || start.LaterCompleted.ID != rows[2].ID {
		t.Fatal(start, err)
	}
	// A publication created after the recovery boundary must not be swept up.
	newer := model.ProtocolDeployment{NodeID: node.ID, Status: "reconciling", DesiredConfigSHA256: strings.Repeat("d", 64), StartedAt: &old}
	if err := store.DB.Create(&newer).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveConfigurationPublication(ctx, start.Deployment, false, "verified later receipt after restart", now); err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		var got model.ProtocolDeployment
		store.DB.First(&got, rows[i].ID)
		want := "superseded"
		if i == 2 {
			want = "succeeded"
		}
		if got.Status != want || got.Error != rows[i].Error || got.AppliedConfigSHA256 != rows[i].AppliedConfigSHA256 {
			t.Fatal(got)
		}
	}
	store.DB.First(&newer, newer.ID)
	if newer.Status != "reconciling" {
		t.Fatal("newer intent was ended", newer)
	}
	var current model.ProtocolEndpoint
	store.DB.First(&current, endpoint.ID)
	if current.MieruPrincipalReady {
		t.Fatal("old recovery enabled current credentials")
	}
}

func TestActiveOlderPublicationCannotBeBypassedByNewerUnknownRecord(t *testing.T) {
	store, node, _ := configurationPublicationFixture(t)
	now := time.Now().UTC()
	active := model.ProtocolDeployment{NodeID: node.ID, Status: "running", StartedAt: &now}
	if err := store.DB.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.ProtocolDeployment{NodeID: node.ID, Status: "reconciling", DesiredConfigSHA256: strings.Repeat("a", 64)}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := store.BeginConfigurationPublication(context.Background(), network.ConfigurationPublicationRequest{NodeID: node.ID}, now)
	if !errors.Is(err, network.ErrKernelOperationRunning) {
		t.Fatal(err)
	}
}

func TestRecoveryRestoresMissingQueueWithoutReplacingOwnedWork(t *testing.T) {
	store, node, _ := configurationPublicationFixture(t)
	now := time.Now().UTC()
	for _, row := range []model.Node{
		{ID: 2, Name: "already queued", IsEnabled: true},
		{ID: 3, Name: "deleted", IsEnabled: true, LifecycleStatus: "archived", ArchivedAt: &now},
		{ID: 4, Name: "fresh operation", IsEnabled: true},
		{ID: 5, Name: "deletion pending", IsEnabled: true, LifecycleStatus: "deleting"},
	} {
		if err := store.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uint{node.ID, 2, 3, 5} {
		if err := store.DB.Create(&model.ProtocolDeployment{NodeID: id, Status: "reconciling", DesiredConfigSHA256: strings.Repeat("a", 64)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DB.Create(&model.ProtocolDeployment{NodeID: 4, Status: "running", StartedAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
	owned := model.NodeConfigPublish{NodeID: 2, Generation: 42, Phase: "connections", LeaseToken: "other-owner", LeaseUntil: now.Add(time.Minute), Suspended: true}
	if err := store.DB.Create(&owned).Error; err != nil {
		t.Fatal(err)
	}
	q := PublicationQueue{DB: store.DB}
	if count, err := q.RecoverUnqueuedPublications(context.Background(), now); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if count, err := q.RecoverUnqueuedPublications(context.Background(), now); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var got model.NodeConfigPublish
	store.DB.First(&got, "node_id = ?", 2)
	if got.Generation != 42 || got.LeaseToken != owned.LeaseToken || !got.Suspended || got.Phase != "connections" {
		t.Fatal(got)
	}
	var restored model.NodeConfigPublish
	store.DB.First(&restored, "node_id = ?", node.ID)
	if !restored.Reconciling || restored.Generation != 1 {
		t.Fatal(restored)
	}
}

func TestRestartDuringConnectionVerificationReentersOnlyOwnedConfiguration(t *testing.T) {
	store, node, _ := configurationPublicationFixture(t)
	now := time.Now().UTC().Add(time.Second)
	ctx := context.Background()
	if err := EnqueuePublication(store.DB, node.ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	q := PublicationQueue{DB: store.DB}
	item, ok, err := q.Claim(ctx, now)
	if err != nil || !ok {
		t.Fatal(item, ok, err)
	}
	if _, err := q.MarkConfigurationApplied(ctx, item, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, item, now, network.ErrConfigurationReapplyRequired); err != nil {
		t.Fatal(err)
	}
	var row model.NodeConfigPublish
	store.DB.First(&row, "node_id = ?", node.ID)
	if row.Phase != "" || row.AppliedConfigSHA256 != "" || !row.Reconciling || row.LeaseToken != "" {
		t.Fatal(row)
	}
	// An older verifier may release its lease but cannot erase the new intent.
	now = now.Add(time.Minute)
	item, ok, err = q.Claim(ctx, now)
	if err != nil || !ok {
		t.Fatal(item, ok, err)
	}
	if err := EnqueuePublication(store.DB, node.ID, 999, 0); err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, item, now, network.ErrConfigurationReapplyRequired); err != nil {
		t.Fatal(err)
	}
	store.DB.First(&row, "node_id = ?", node.ID)
	if row.Generation != item.Generation+1 || row.EndpointID != 999 {
		t.Fatal(row)
	}
}
