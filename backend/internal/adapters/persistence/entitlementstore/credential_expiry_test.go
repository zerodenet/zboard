package entitlementstore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestCredentialExpiryCommitsRevocationAndPublicationAtomically(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "credential-expiry.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service := func(failure error) entitlements.CredentialExpiry {
		return entitlements.CredentialExpiry{Repository: CredentialExpiry{DB: db, Publish: func(tx *gorm.DB, nodeID, endpointID, actor uint) error {
			if failure != nil {
				return failure
			}
			return tx.Create(&model.NodeConfigPublish{NodeID: nodeID, EndpointID: endpointID, RequestedBy: actor, Generation: 1}).Error
		}}}
	}
	seed := func(id uint) model.Subscription {
		if err := db.Create(&model.Node{ID: id, Name: fmt.Sprintf("node-%d", id), Config: "{}"}).Error; err != nil {
			t.Fatal(err)
		}
		sub := model.Subscription{UserID: id, NodeGroupID: id, Status: "active", StartAt: now.Add(-time.Hour), EndAt: now.Add(-time.Minute), FlowTotal: 1}
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ProtocolCredential{SubscriptionID: sub.ID, UserID: id, ProtocolEndpointID: id, NodeID: id, CredentialID: fmt.Sprintf("credential-%d", id), PrincipalKey: fmt.Sprintf("principal-%d", id), Secret: "secret", Status: "active", ExpiresAt: now.Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
		return sub
	}
	first := seed(101)
	if due, err := service(nil).HasDue(context.Background(), now); err != nil || !due {
		t.Fatalf("due=%t err=%v", due, err)
	}
	if expired, err := service(nil).ExpireDue(context.Background(), now, 10); err != nil || len(expired) != 1 {
		t.Fatalf("expired=%+v err=%v", expired, err)
	}
	var persisted model.Subscription
	if err := db.First(&persisted, first.ID).Error; err != nil || persisted.Status != "expired" {
		t.Fatalf("subscription = %+v err=%v", persisted, err)
	}
	var publication model.NodeConfigPublish
	if err := db.First(&publication, 101).Error; err != nil || publication.EndpointID != 101 {
		t.Fatalf("publication = %+v err=%v", publication, err)
	}

	second := seed(202)
	failure := errors.New("publication failed")
	if _, err := service(failure).ExpireDue(context.Background(), now, 10); !errors.Is(err, failure) {
		t.Fatalf("publication error = %v", err)
	}
	persisted = model.Subscription{}
	if err := db.First(&persisted, second.ID).Error; err != nil || persisted.Status != "active" {
		t.Fatalf("rollback subscription = %+v err=%v", persisted, err)
	}
}
