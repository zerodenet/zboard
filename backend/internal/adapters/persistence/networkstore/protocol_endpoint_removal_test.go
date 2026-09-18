package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func protocolEndpointRemovalFixture(t *testing.T, db *gorm.DB) (network.ProtocolEndpointRemoval, model.ProtocolEndpoint, model.NetworkEntry, model.NodeGroup) {
	t.Helper()
	for _, row := range []any{
		&model.User{ID: 1, Email: "admin@example.test", Password: "unused", Status: "active", IsAdmin: true},
		&model.Node{ID: 10, Name: "landing", Address: "192.0.2.10", Config: "{}"},
		&model.Node{ID: 11, Name: "entry", Address: "192.0.2.11", Config: "{}"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpoint := model.ProtocolEndpoint{ID: 20, NodeID: 10, Name: "vless", RuntimeKey: "00000000-0000-4000-8000-000000000020", Protocol: "vless", Address: "landing.example", Port: 443, PublicPort: 443, ServerConfig: "encrypted", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{Name: "front", NodeID: 11, EndpointID: endpoint.ID, Address: "front.example", Port: 8443, PublicPort: 8443, Enabled: true}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{ID: 30, Name: "group", Code: "group", IsEnabled: true, Revision: 1}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID},
		&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID},
		&model.ManagedCertificate{ID: 40, NodeID: 10, Name: "certificate", Domains: `[]`, ContactEmail: "admin@example.test", Status: "active"},
		&model.CertificateProtocolEndpoint{ManagedCertificateID: 40, ProtocolEndpointID: endpoint.ID},
		&model.ProtocolCredential{ID: 50, UserID: 2, SubscriptionID: 60, NodeID: 10, ProtocolEndpointID: endpoint.ID, Secret: "encrypted-secret", Status: "active", ListenPort: 443, PublicPort: 443, ExpiresAt: time.Now().UTC().Add(time.Hour)},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 16, 9, 30, 0, 0, time.UTC)
	return network.ProtocolEndpointRemoval{Store: ProtocolEndpointRemoval{DB: db}, Now: func() time.Time { return now }}, endpoint, entry, group
}

func TestProtocolEndpointRemovalCommitsTopologyCredentialsAuditAndPublications(t *testing.T) {
	db, _ := administrationFixture(t)
	service, endpoint, entry, group := protocolEndpointRemovalFixture(t, db)
	removed, err := service.Remove(context.Background(), 1, endpoint.ID)
	if err != nil || !removed.Deleted || !removed.RuntimeCleanupQueued || removed.RemovedFromRuntime {
		t.Fatalf("removed=%+v error=%v", removed, err)
	}
	for table, query := range map[string]*gorm.DB{
		"endpoint":         db.Model(&model.ProtocolEndpoint{}).Where("id = ?", endpoint.ID),
		"entry":            db.Model(&model.NetworkEntry{}).Where("id = ?", entry.ID),
		"group endpoint":   db.Model(&model.NodeGroupEndpoint{}).Where("protocol_endpoint_id = ?", endpoint.ID),
		"group entry":      db.Model(&model.NodeGroupNetworkEntry{}).Where("network_entry_id = ?", entry.ID),
		"certificate link": db.Model(&model.CertificateProtocolEndpoint{}).Where("protocol_endpoint_id = ?", endpoint.ID),
	} {
		var count int64
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d error=%v", table, count, err)
		}
	}
	var credential model.ProtocolCredential
	if err := db.First(&credential, 50).Error; err != nil || credential.Status != "revoked" || credential.RevokedAt == nil || !credential.RevokedAt.Equal(time.Date(2026, 9, 16, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("credential=%+v error=%v", credential, err)
	}
	if err := db.First(&group, group.ID).Error; err != nil || group.Revision != 3 {
		t.Fatalf("group=%+v error=%v", group, err)
	}
	var publications []model.NodeConfigPublish
	if err := db.Order("node_id").Find(&publications).Error; err != nil || len(publications) != 2 || publications[0].NodeID != 10 || publications[1].NodeID != 11 {
		t.Fatalf("publications=%+v error=%v", publications, err)
	}
	var audit model.AuditLog
	if err := db.Where("action = ? AND target = ?", "protocol_endpoint.delete", "protocol_endpoint:20").First(&audit).Error; err != nil || audit.UserID == nil || *audit.UserID != 1 {
		t.Fatalf("audit=%+v error=%v", audit, err)
	}
}

func TestProtocolEndpointRemovalRejectsConflictRevocationAndDeletingNode(t *testing.T) {
	for _, scenario := range []string{"running", "revoked", "deleting", "audit"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			service, endpoint, entry, _ := protocolEndpointRemovalFixture(t, db)
			switch scenario {
			case "running":
				if err := db.Create(&model.ProtocolDeployment{ProtocolEndpointID: endpoint.ID, NodeID: endpoint.NodeID, Status: "running"}).Error; err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
					t.Fatal(err)
				}
			case "deleting":
				if err := db.Model(&model.Node{}).Where("id = ?", endpoint.NodeID).Update("lifecycle_status", "deleting").Error; err != nil {
					t.Fatal(err)
				}
			case "audit":
				if err := db.Callback().Create().Before("gorm:create").Register("fail_endpoint_removal_audit", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
						tx.AddError(errors.New("audit unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Create().Remove("fail_endpoint_removal_audit")
			}
			_, err := service.Remove(context.Background(), 1, endpoint.ID)
			switch scenario {
			case "running":
				if !errors.Is(err, network.ErrProtocolEndpointRemovalConflict) {
					t.Fatalf("error=%v", err)
				}
			case "revoked":
				if !errors.Is(err, network.ErrResourcePermission) {
					t.Fatalf("error=%v", err)
				}
			case "deleting":
				if !errors.Is(err, network.ErrProtocolEndpointResourceDeleting) {
					t.Fatalf("error=%v", err)
				}
			case "audit":
				if err == nil {
					t.Fatal("audit failure committed")
				}
			}
			var endpointCount, entryCount, publicationCount int64
			if err := db.Model(&model.ProtocolEndpoint{}).Where("id = ?", endpoint.ID).Count(&endpointCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.NetworkEntry{}).Where("id = ?", entry.ID).Count(&entryCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.NodeConfigPublish{}).Count(&publicationCount).Error; err != nil {
				t.Fatal(err)
			}
			if endpointCount != 1 || entryCount != 1 || publicationCount != 0 {
				t.Fatalf("partial removal endpoint=%d entry=%d publications=%d", endpointCount, entryCount, publicationCount)
			}
		})
	}
}
