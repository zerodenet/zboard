package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestDNSDeletionAndNativeOperationBoundaries(t *testing.T) {
	db, _ := administrationFixture(t)
	account := model.ProviderAccount{Name: "provider", ProviderKey: "cloudflare", CredentialCiphertext: "opaque", Status: "active", Revision: 1}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	deletion := network.DNSDeletion{Repository: DNSDeletion{DB: db}}
	untouched, err := deletion.Prepare(context.Background(), network.DNSDeletionInput{RecordID: 10, ProviderAccountID: account.ID})
	if err != nil || untouched.DeleteRemote {
		t.Fatalf("untouched=%+v error=%v", untouched, err)
	}
	if err := db.Create(&model.ProviderOperation{ProviderAccountID: account.ID, ResourceType: "dns_record", ResourceID: 10, OperationType: "sync", Status: "failed", Phase: "applying_record"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := deletion.Prepare(context.Background(), network.DNSDeletionInput{RecordID: 10, ProviderAccountID: account.ID}); !errors.Is(err, network.ErrDNSDeletionRemoteIdentity) {
		t.Fatalf("uncertain deletion error=%v", err)
	}
	prepared, err := deletion.Prepare(context.Background(), network.DNSDeletionInput{RecordID: 10, ProviderAccountID: account.ID, ProviderZoneID: "zone", ProviderRecordID: "record"})
	if err != nil || !prepared.DeleteRemote || prepared.CredentialCiphertext != "opaque" {
		t.Fatalf("prepared=%+v error=%v", prepared, err)
	}

	now := time.Now().UTC()
	operation := model.ProviderOperation{ProviderAccountID: account.ID, ResourceType: "dns_record", ResourceID: 11, OperationType: "sync", Status: "succeeded", Phase: "completed", StartedAt: &now}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	status := network.NativeOperationStatus{Repository: NativeOperationStatus{DB: db}}
	got, err := status.Status(context.Background(), network.DNSOperation, operation.ID)
	if err != nil || got != "succeeded" {
		t.Fatalf("status=%q error=%v", got, err)
	}
}

func TestEventCredentialBoundaryRejectsRevokedAndMissingNodes(t *testing.T) {
	db, _ := administrationFixture(t)
	active := model.Node{Name: "active", NodeCredential: "ciphertext"}
	revokedAt := time.Now().UTC()
	revoked := model.Node{Name: "revoked", NodeCredential: "old", NodeCredentialRevokedAt: &revokedAt}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&revoked).Error; err != nil {
		t.Fatal(err)
	}
	service := network.EventCredentials{Repository: EventCredentials{DB: db}}
	got, err := service.Load(context.Background(), active.ID)
	if err != nil || got.ID != active.ID || got.Credential != "ciphertext" {
		t.Fatalf("active credential=%+v error=%v", got, err)
	}
	for _, id := range []uint{revoked.ID, 99999} {
		if _, err := service.Load(context.Background(), id); !errors.Is(err, network.ErrEventCredentialUnavailable) {
			t.Fatalf("node %d error=%v", id, err)
		}
	}
}
