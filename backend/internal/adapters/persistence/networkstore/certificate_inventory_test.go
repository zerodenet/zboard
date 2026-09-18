package networkstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCertificateInventoryReturnsLatestOperationAndBindings(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "certificate-inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	node := model.Node{Name: "edge-a", Config: "{}", IsEnabled: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	certificate := model.ManagedCertificate{
		NodeID: node.ID, Name: "public", Domains: `["edge.example"]`,
		ContactEmail: "ops@example.com", Environment: "production", ChallengeType: "http-01", Status: "active",
	}
	if err := db.Create(&certificate).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{
		NodeID: node.ID, Name: "https", RuntimeKey: "certificate-inventory-endpoint", Protocol: "vless",
		Address: "edge.example", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]", IsActive: true,
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CertificateProtocolEndpoint{ManagedCertificateID: certificate.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	operations := []model.CertificateOperation{
		{ManagedCertificateID: certificate.ID, NodeID: node.ID, OperationType: "issue", Status: "failed", Phase: "finish"},
		{ManagedCertificateID: certificate.ID, NodeID: node.ID, OperationType: "renew", Status: "succeeded", Phase: "finish"},
	}
	if err := db.Create(&operations).Error; err != nil {
		t.Fatal(err)
	}

	service := network.CertificateInventory{Repository: CertificateInventory{DB: db}}
	page, err := service.List(context.Background(), network.CertificateInventoryQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	item := page.Items[0]
	if item.NodeName != node.Name || item.UsageCount != 1 || item.LatestOperation == nil || item.LatestOperation.ID != operations[1].ID {
		t.Fatalf("item = %+v", item)
	}
	detail, err := service.Detail(context.Background(), certificate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.ProtocolEndpointIDs) != 1 || detail.ProtocolEndpointIDs[0] != endpoint.ID {
		t.Fatalf("detail bindings = %v", detail.ProtocolEndpointIDs)
	}
	bindings, err := service.Bindings(context.Background(), []uint{endpoint.ID})
	if err != nil || bindings[endpoint.ID] != certificate.ID {
		t.Fatalf("bindings = %v err=%v", bindings, err)
	}
	if _, err := service.Detail(context.Background(), certificate.ID+1000); !errors.Is(err, network.ErrCertificateNotFound) {
		t.Fatalf("missing detail error = %v", err)
	}
}

func TestCertificateLifecyclePreparesOpaqueExecutionSnapshot(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "certificate-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	provider := model.ProviderAccount{Name: "dns", ProviderKey: "edge-dns", Status: "active", Capabilities: `["certificate.issue"]`, CredentialCiphertext: "provider-ciphertext"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "certificate-node", Config: "{}", IsEnabled: true, SSHHost: "ssh.example", SSHPort: 22, SSHUser: "root", SSHAuthMethod: "password", SSHPwd: "ssh-ciphertext"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	certificate := model.ManagedCertificate{NodeID: node.ID, ProviderAccountID: &provider.ID, Name: "dns", Domains: `["edge.example"]`, ContactEmail: "ops@example.com", Environment: "production", ChallengeType: "dns-01", Status: "issuing"}
	if err := db.Create(&certificate).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "tls", RuntimeKey: "certificate-execution-endpoint", Protocol: "vless", Address: "edge.example", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]", IsActive: true}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CertificateProtocolEndpoint{ManagedCertificateID: certificate.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	operation := model.CertificateOperation{ManagedCertificateID: certificate.ID, NodeID: node.ID, OperationType: "issue", Status: "running", Phase: "queued"}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := (network.CertificateLifecycle{Repository: CertificateLifecycle{DB: db}}).Prepare(context.Background(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Operation.ID != operation.ID || snapshot.Certificate.ID != certificate.ID || snapshot.Node.SSHPwdCiphertext != node.SSHPwd || snapshot.Provider == nil || snapshot.Provider.ProviderKey != "edge-dns" || len(snapshot.Provider.Capabilities) != 1 || snapshot.Provider.Capabilities[0] != "certificate.issue" || snapshot.Provider.CredentialCiphertext != provider.CredentialCiphertext || snapshot.BindingProtocolEndpointID != endpoint.ID {
		t.Fatalf("execution snapshot = %+v", snapshot)
	}
}

func TestCertificateLifecycleAcceptsCertificateOnlyProvider(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "certificate-provider.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "certificate-admin@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	node := model.Node{Name: "edge", Config: "{}", IsEnabled: true}
	provider := model.ProviderAccount{Name: "issuer", ProviderKey: "edge-ca", Status: "active", Capabilities: `["certificate.issue"]`, CredentialCiphertext: "ciphertext"}
	for _, row := range []any{&admin, &node, &provider} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	created, _, err := (CertificateLifecycle{DB: db}).CreateCertificate(context.Background(), admin.ID, network.CertificateRecord{
		NodeID: node.ID, ProviderAccountID: &provider.ID, Name: "plugin certificate", Domains: `["edge.example.test"]`,
		ContactEmail: "ops@example.test", Environment: "production", ChallengeType: "dns-01", Status: "pending", AutoRenew: true, RenewBeforeDays: 30,
	})
	if err != nil || created.ID == 0 {
		t.Fatalf("created=%+v error=%v", created, err)
	}
}
