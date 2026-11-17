package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeletingNodeDoesNotFalselyConfirmConnectionRevocation(t *testing.T) {
	h := openPublishTestHandlers(t, filepath.Join(t.TempDir(), "deleting.db"))
	if err := h.db.Create(&model.Node{ID: 1, Name: "deleting", Config: "{}", LifecycleStatus: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := enqueueNodeConfigPublishOnly(h.db, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Second)
	item, ok, err := claimNodeConfigPublish(h.db, now)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = h.services.MarkPublicationConfigurationApplied(context.Background(), publicationCapability(item), strings.Repeat("a", 64))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	// A previously accepted publication cannot turn a later terminal lifecycle
	// into evidence that the original native connections were actually closed.
	if err := h.db.Model(&model.Node{}).Where("id = 1").Update("lifecycle_status", "deleting").Error; err != nil {
		t.Fatal(err)
	}
	item.Phase = "connections"
	item.AppliedConfigSHA256 = strings.Repeat("a", 64)
	failure := h.publishQueuedNode(context.Background(), item)
	if !errors.Is(failure, network.ErrConnectionRevocationPending) {
		t.Fatal("inactive node became proof of closure", failure)
	}
	if err := finishNodeConfigPublish(h.db, item, now, failure); err != nil {
		t.Fatal(err)
	}
	var held model.NodeConfigPublish
	if err := h.db.First(&held, 1).Error; err != nil || held.Phase != "connections" {
		t.Fatal(held, err)
	}
	var count int64
	if err := h.db.Model(&model.AuditLog{}).Where("action = ?", "node.connection_revocation_verified").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("false closure audit", count, err)
	}
}
