package networkstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func protocolEndpointRemovalFixture(t *testing.T, db *gorm.DB) (network.ProtocolEndpointRemoval, model.ProtocolEndpoint, model.NetworkEntry, model.NodeGroup) {
	t.Helper()
	for _, row := range []any{
		&model.User{ID: 2, Email: "member@example.test", Password: "unused", Status: "active"},
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
		&model.Plan{ID: 59, Name: "fixture", Slug: "fixture", NodeGroupID: group.ID, IsActive: false},
		&model.PlanSKU{ID: 58, PlanID: 59, Code: "fixture", Name: "fixture"},
		&model.Subscription{ID: 60, UserID: 2, PlanID: 59, PlanSKUID: 58, NodeGroupID: group.ID, Status: "expired", StartAt: time.Now().UTC().Add(-24 * time.Hour), EndAt: time.Now().UTC().Add(-time.Hour), Config: "{}"},
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
	if err := db.Model(&model.Plan{}).Where("id = ?", 59).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 16, 9, 30, 0, 0, time.UTC)
	return network.ProtocolEndpointRemoval{Store: ProtocolEndpointRemoval{DB: db}, Now: func() time.Time { return now }}, endpoint, entry, group
}

func TestProtocolEndpointRemovalCommitsTopologyCredentialsAuditAndPublications(t *testing.T) {
	db, _ := administrationFixture(t)
	service, endpoint, entry, group := protocolEndpointRemovalFixture(t, db)
	if err := db.Model(&group).Update("is_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
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

func TestProtocolEndpointRemovalRejectsRevocationCompetingNodeDeletionAndFailedAudit(t *testing.T) {
	rootDB, _ := administrationFixture(t)
	for _, scenario := range []string{"revoked", "deleting", "audit"} {
		t.Run(scenario, func(t *testing.T) {
			db := rootDB.Begin()
			t.Cleanup(func() { db.Rollback() })
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

func TestProtocolDeletionRetainsFinancialIdentitiesAndIsIdempotent(t *testing.T) {
	db, _ := administrationFixture(t)
	service, endpoint, _, group := protocolEndpointRemovalFixture(t, db)
	if err := db.Model(&group).Update("is_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	traffic := model.TrafficRecord{UserID: 2, SubscriptionID: 60, NodeID: 10, ProtocolEndpointID: endpoint.ID, ReportID: "history", Nonce: "history", RawBytes: 10, UsedBytes: 20, ProtocolMultiplierMilli: 2000, At: now, Meta: "unchanged"}
	usage := model.FlowUsage{NodeID: 10, ProtocolEndpointID: endpoint.ID, ProtocolCredentialID: 50, SubscriptionID: 60, FlowID: "history", PrincipalKey: "history", RawBytes: 10, UsedBytes: 20, Status: "active", LastSeenAt: now}
	deployment := model.ProtocolDeployment{NodeID: 10, ProtocolEndpointID: endpoint.ID, Status: "failed", Output: "immutable runtime evidence"}
	for _, row := range []any{&traffic, &usage, &deployment} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Read database-normalized timestamps before comparing exact immutable facts.
	db.First(&traffic, traffic.ID)
	db.First(&usage, usage.ID)
	db.First(&deployment, deployment.ID)
	guard := func(tx *gorm.DB) {
		if tx.Statement.Table == "traffic_records" || tx.Statement.Table == "flow_usages" {
			tx.AddError(errors.New("deletion accessed financial history"))
		}
	}
	db.Callback().Query().Before("gorm:query").Register("no_history_during_delete", guard)
	db.Callback().Update().Before("gorm:update").Register("no_history_during_delete", guard)
	db.Callback().Delete().Before("gorm:delete").Register("no_history_during_delete", guard)
	for range 2 {
		result, err := service.Remove(t.Context(), 1, endpoint.ID)
		if err != nil || !result.Deleted {
			t.Fatal(result, err)
		}
	}
	db.Callback().Query().Remove("no_history_during_delete")
	db.Callback().Update().Remove("no_history_during_delete")
	db.Callback().Delete().Remove("no_history_during_delete")
	var retained model.ProtocolEndpoint
	if err := db.Unscoped().First(&retained, endpoint.ID).Error; err != nil || !retained.DeletedAt.Valid || retained.IsActive {
		t.Fatal(retained, err)
	}
	var afterTraffic model.TrafficRecord
	var afterUsage model.FlowUsage
	var afterDeployment model.ProtocolDeployment
	if err := db.First(&afterTraffic, traffic.ID).Error; err != nil || !reflect.DeepEqual(traffic, afterTraffic) {
		t.Fatal("ledger changed", err)
	}
	if err := db.First(&afterUsage, usage.ID).Error; err != nil || !reflect.DeepEqual(usage, afterUsage) {
		t.Fatal("cursor changed", err)
	}
	if err := db.First(&afterDeployment, deployment.ID).Error; err != nil || !reflect.DeepEqual(deployment, afterDeployment) {
		t.Fatal("publication evidence changed", err)
	}
	var audits int64
	db.Model(&model.AuditLog{}).Where("action = ?", "protocol_endpoint.delete").Count(&audits)
	if audits != 1 {
		t.Fatal("duplicate deletion side effects", audits)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Remove(t.Context(), 1, endpoint.ID); !errors.Is(err, network.ErrResourcePermission) {
		t.Fatal("idempotency bypassed current authorization", err)
	}
}
func TestProtocolDeletionWarnsWithoutRequiringOtherResourcesToBeDeleted(t *testing.T) {
	rootDB, _ := administrationFixture(t)
	for _, scenario := range []string{"direct group", "forward group", "published plan", "subscription", "child protocol", "other endpoint publication", "expired lease", "archived parent"} {
		t.Run(scenario, func(t *testing.T) {
			db := rootDB.Begin()
			t.Cleanup(func() { db.Rollback() })
			service, endpoint, entry, group := protocolEndpointRemovalFixture(t, db)
			db.Model(&group).Update("is_enabled", false)
			check, err := service.Check(t.Context(), 1, endpoint.ID)
			if err != nil || len(check.Blockers) != 0 {
				t.Fatal("initial preview", check, err)
			}
			switch scenario {
			case "direct group":
				db.Model(&group).Update("is_enabled", true)
			case "forward group":
				db.Where("protocol_endpoint_id = ?", endpoint.ID).Delete(&model.NodeGroupEndpoint{})
				db.Model(&group).Update("is_enabled", true)
			case "published plan":
				db.Model(&model.Plan{}).Where("id = ?", 59).Update("is_active", true)
			case "subscription":
				db.Model(&model.Subscription{}).Where("id = ?", 60).Updates(map[string]any{"status": "active", "end_at": time.Now().Add(time.Hour)})
			case "child protocol":
				child := endpoint
				child.ID = 0
				child.RuntimeKey = "00000000-0000-4000-8000-000000000021"
				child.ParentProtocolID = &endpoint.ID
				if err := db.Create(&child).Error; err != nil {
					t.Fatal(err)
				}
			case "other endpoint publication":
				if err := db.Create(&model.ProtocolDeployment{NodeID: endpoint.NodeID, Status: "running"}).Error; err != nil {
					t.Fatal(err)
				}
			case "expired lease":
				if err := db.Create(&model.NodeConfigPublish{NodeID: entry.NodeID, Generation: 1, LeaseToken: "unconfirmed", LeaseUntil: time.Now().Add(-time.Hour), NextAttemptAt: time.Now()}).Error; err != nil {
					t.Fatal(err)
				}
			case "archived parent":
				db.Model(&model.Node{}).Where("id = ?", endpoint.NodeID).Update("lifecycle_status", "archived")
			}
			check, err = service.Check(t.Context(), 1, endpoint.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "archived parent" {
				if !check.AlreadyDeleted {
					t.Fatal(check)
				}
				result, err := service.Remove(t.Context(), 1, endpoint.ID)
				if err != nil || result.RuntimeCleanupQueued {
					t.Fatal(result, err)
				}
				return
			}
			if len(check.Blockers) != 0 || len(check.Warnings) == 0 {
				t.Fatal("business references blocked deletion or hid its impact", scenario, check)
			}
			if result, err := service.Remove(t.Context(), 1, endpoint.ID); err != nil || !result.Deleted {
				t.Fatal("independent protocol deletion failed", scenario, result, err)
			}
			var server model.Node
			if err := db.First(&server, endpoint.NodeID).Error; err != nil || server.LifecycleStatus == "archived" {
				t.Fatal("protocol deletion removed server", err)
			}
			if scenario == "child protocol" {
				var child model.ProtocolEndpoint
				if err := db.Where("id <> ?", endpoint.ID).First(&child).Error; err != nil || child.ParentProtocolID != nil {
					t.Fatal("related protocol was deleted or retained stale parent", err)
				}
			}
			if scenario == "expired lease" {
				var q model.NodeConfigPublish
				if err := db.First(&q, entry.NodeID).Error; err != nil || q.LeaseToken != "unconfirmed" || q.Generation <= 1 {
					t.Fatal("deletion lost executing lease or withdrawal generation", q, err)
				}
			}

		})
	}
}
func TestProtocolInventoriesExcludeDeletedParentsAndIdentities(t *testing.T) {
	rootDB, _ := administrationFixture(t)
	for _, kind := range []string{"all", "forward", "listener"} {
		for _, state := range []string{"parent", "protocol"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				db := rootDB.Begin()
				t.Cleanup(func() { db.Rollback() })
				_, endpoint, entry, group := protocolEndpointRemovalFixture(t, db)
				if state == "parent" {
					db.Model(&model.Node{}).Where("id = ?", endpoint.NodeID).Update("lifecycle_status", "archived")
				} else {
					db.Model(&endpoint).Updates(map[string]any{"deleted_at": time.Now(), "is_active": false})
				}
				for _, query := range []network.ProtocolEndpointInventoryQuery{{ServiceKind: kind, IncludeStatusFacets: true}, {ServiceKind: kind, GroupID: group.ID}, {ServiceKind: kind, NodeID: endpoint.NodeID}, {ServiceKind: kind, IDs: []uint{endpoint.ID, entry.ID}}} {
					page, err := (Inventory{DB: db}).ListProtocolEndpoints(t.Context(), query)
					if err != nil || page.Total != 0 || len(page.Items) != 0 || page.Facets.All != 0 {
						t.Fatal(query, page, err)
					}
				}
			})
		}
	}
}

func TestDeletionGroupsAcceptOnlySurvivingDeliveryPaths(t *testing.T) {
	root, _ := administrationFixture(t)
	for _, kind := range []string{"direct", "forward", "disabled node", "deleted endpoint"} {
		t.Run(kind, func(t *testing.T) {
			db := root.Begin()
			t.Cleanup(func() { db.Rollback() })
			service, endpoint, _, group := protocolEndpointRemovalFixture(t, db)
			alternate := endpoint
			alternate.ID = 21
			alternate.RuntimeKey = "00000000-0000-4000-8000-000000000021"
			if err := db.Create(&alternate).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "forward" {
				entry := model.NetworkEntry{NodeID: 11, EndpointID: alternate.ID, Name: "alternate", Address: "front.test", Port: 8444, PublicPort: 8444, Enabled: true}
				if err := db.Create(&entry).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: alternate.ID}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if kind == "disabled node" {
				db.Model(&model.Node{}).Where("id = ?", alternate.NodeID).Update("is_enabled", false)
			}
			if kind == "deleted endpoint" {
				db.Model(&alternate).Update("deleted_at", time.Now())
			}
			check, err := service.Check(t.Context(), 1, endpoint.ID)
			if err != nil {
				t.Fatal(err)
			}
			blocked := kind == "disabled node" || kind == "deleted endpoint"
			if (len(check.Warnings) > 0) != blocked {
				t.Fatal("incorrect surviving path", kind, check)
			}
		})
	}
}
func TestEntryDeletionWarnsAboutEmptyGroupAndKeepsParent(t *testing.T) {
	db, _ := administrationFixture(t)
	_, endpoint, entry, _ := protocolEndpointRemovalFixture(t, db)
	store := TopologyRemoval{DB: db}
	// With a direct membership the forwarding entry is independently removable.
	check, err := store.CheckEntryDeletion(t.Context(), 1, entry.ID)
	if err != nil || len(check.Blockers) != 0 {
		t.Fatal(check, err)
	}
	if err := db.Where("protocol_endpoint_id = ?", endpoint.ID).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
		t.Fatal(err)
	}
	check, err = store.CheckEntryDeletion(t.Context(), 1, entry.ID)
	if err != nil || len(check.Blockers) != 0 || len(check.Warnings) == 0 {
		t.Fatal(check, err)
	}
	if err := store.RemoveEntry(t.Context(), 1, entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&endpoint, endpoint.ID).Error; err != nil {
		t.Fatal("entry removal deleted parent", err)
	}
}
