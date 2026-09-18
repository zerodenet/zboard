package networkstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRuntimeConfigurationLoadsConsistentNodeProjection(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "runtime-configuration.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	installation := model.Installation{ID: 1, SiteName: "Runtime", SiteURL: "https://panel.example.test", InstalledAt: now}
	if err := db.Save(&installation).Error; err != nil {
		t.Fatal(err)
	}
	nodeA := model.Node{ID: 1, Name: "runtime-a", IsEnabled: true}
	nodeB := model.Node{ID: 2, Name: "runtime-b", IsEnabled: true}
	group := model.NodeGroup{ID: 1, Name: "runtime-group", Code: "runtime-group", IsEnabled: true, Revision: 1}
	user := model.User{ID: 1, Email: "runtime@example.test", Password: "hash", Status: "active"}
	for _, record := range []interface{}{&nodeA, &nodeB, &group, &user} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpointA := model.ProtocolEndpoint{ID: 1, NodeID: nodeA.ID, Name: "runtime-vless", RuntimeKey: "runtime-vless", Protocol: "vless", Address: "a.example.test", Port: 443, PublicPort: 8443, ServerConfig: "cipher-a", IsActive: true, SortOrder: 2}
	endpointB := model.ProtocolEndpoint{ID: 2, NodeID: nodeB.ID, Name: "runtime-vmess", RuntimeKey: "runtime-vmess", Protocol: "vmess", Address: "b.example.test", Port: 444, PublicPort: 8444, ServerConfig: "cipher-b", IsActive: true}
	for _, record := range []interface{}{&endpointA, &endpointB} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, membership := range []model.NodeGroupEndpoint{{NodeGroupID: group.ID, ProtocolEndpointID: endpointA.ID}, {NodeGroupID: group.ID, ProtocolEndpointID: endpointB.ID}} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatal(err)
		}
	}
	subscription := model.Subscription{ID: 1, UserID: user.ID, NodeGroupID: group.ID, Status: "active", EndAt: now.Add(time.Hour), FlowTotal: 1024, SpeedLimitMbps: 50, DeviceLimit: 3, UpdatedAt: now.Add(-time.Minute)}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	for _, credential := range []model.ProtocolCredential{
		{ID: 1, SubscriptionID: subscription.ID, UserID: user.ID, ProtocolEndpointID: endpointA.ID, NodeID: nodeA.ID, CredentialID: "runtime-credential-a", PrincipalKey: "runtime-principal-a", Secret: "secret-a", ListenPort: 443, PublicPort: 8443, Status: "active", ExpiresAt: now.Add(time.Hour)},
		{ID: 2, SubscriptionID: subscription.ID, UserID: user.ID, ProtocolEndpointID: endpointB.ID, NodeID: nodeB.ID, CredentialID: "runtime-credential-b", PrincipalKey: "runtime-principal-b", Secret: "secret-b", ListenPort: 444, PublicPort: 8444, Status: "active", ExpiresAt: now.Add(time.Hour)},
	} {
		if err := db.Create(&credential).Error; err != nil {
			t.Fatal(err)
		}
	}
	notAfter := now.Add(24 * time.Hour)
	certificate := model.ManagedCertificate{ID: 1, NodeID: nodeA.ID, Name: "runtime-cert", Domains: `["a.example.test"]`, ContactEmail: "ops@example.test", Status: "active", CertPath: "/cert.pem", KeyPath: "/key.pem", NotAfter: &notAfter, Revision: 1}
	if err := db.Create(&certificate).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CertificateProtocolEndpoint{ManagedCertificateID: certificate.ID, ProtocolEndpointID: endpointA.ID}).Error; err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{ID: 1, Name: "runtime-entry", NodeID: nodeA.ID, EndpointID: endpointB.ID, Network: "tcp_udp", Address: "entry.example.test", Port: 9443, PublicPort: 9443, Enabled: true, Revision: 1}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	snapshot, err := (RuntimeConfiguration{DB: db}).LoadRuntimeConfiguration(context.Background(), nodeA.ID, now, []string{"vless", "vmess"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SiteURL != installation.SiteURL || len(snapshot.Endpoints) != 1 {
		t.Fatalf("snapshot mismatch: %+v", snapshot)
	}
	endpoint := snapshot.Endpoints[0]
	if endpoint.ID != endpointA.ID || endpoint.ServerConfig != "cipher-a" || endpoint.ActiveSubscriptionCount != 1 || len(endpoint.Credentials) != 1 {
		t.Fatalf("endpoint projection mismatch: %+v", endpoint)
	}
	credential := endpoint.Credentials[0]
	if credential.Secret != "secret-a" || credential.SpeedLimitMbps != 50 || credential.DeviceLimit != 3 || credential.SoleActiveCredential {
		t.Fatalf("credential projection mismatch: %+v", credential)
	}
	if endpoint.Certificate == nil || endpoint.Certificate.CertPath != "/cert.pem" || endpoint.Certificate.NotAfter == nil || !endpoint.Certificate.NotAfter.Equal(notAfter) {
		t.Fatalf("certificate projection mismatch: %+v", endpoint.Certificate)
	}
	if len(snapshot.NetworkEntries) != 1 || snapshot.NetworkEntries[0].Landing.Protocol != "vmess" || !snapshot.NetworkEntries[0].Landing.NodeExists || !snapshot.NetworkEntries[0].Landing.NodeEnabled {
		t.Fatalf("network entry projection mismatch: %+v", snapshot.NetworkEntries)
	}
}

func TestRuntimeConfigurationHonorsCancellation(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "runtime-configuration-canceled.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (RuntimeConfiguration{DB: db}).LoadRuntimeConfiguration(ctx, 1, time.Now().UTC(), []string{"vless"})
	if err == nil {
		t.Fatal("canceled runtime configuration load succeeded")
	}
}
