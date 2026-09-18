package networkstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type protocolEndpointMutationCipher struct{}

func (protocolEndpointMutationCipher) Encrypt(value string) (string, error) {
	return "enc:" + value, nil
}
func (protocolEndpointMutationCipher) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(value, "enc:"), nil
}

func protocolEndpointMutationService(db *gorm.DB) network.ProtocolEndpointMutations {
	nextRuntimeKey := 0
	return network.ProtocolEndpointMutations{
		Repository: ProtocolEndpointMutations{DB: db}, Cipher: protocolEndpointMutationCipher{},
		Now: func() time.Time { return time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC) },
		NewRuntimeKey: func() string {
			nextRuntimeKey++
			return fmt.Sprintf("00000000-0000-4000-8000-%012d", nextRuntimeKey)
		},
	}
}

func seedProtocolEndpointMutationAuthority(t *testing.T, db *gorm.DB) (model.Node, model.Node, model.NodeGroup, model.ManagedCertificate) {
	t.Helper()
	notAfter := time.Date(2027, 9, 16, 0, 0, 0, 0, time.UTC)
	rows := []any{
		&model.User{ID: 1, Email: "admin@example.test", Password: "unused", Status: "active", IsAdmin: true},
		&model.Node{ID: 10, Name: "node-a", Address: "192.0.2.10", Config: "{}", LifecycleStatus: "active"},
		&model.Node{ID: 20, Name: "node-b", Address: "192.0.2.20", Config: "{}", LifecycleStatus: "active"},
		&model.NodeGroup{ID: 30, Name: "group", Code: "group", IsEnabled: true, Revision: 1},
		&model.ManagedCertificate{ID: 40, NodeID: 10, Name: "cert", Domains: `[]`, ContactEmail: "admin@example.test", Status: "active", NotAfter: &notAfter},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Subscription{ID: 50, UserID: 2, NodeGroupID: 30, Status: "active", EndAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), FlowTotal: 1000}).Error; err != nil {
		t.Fatal(err)
	}
	return model.Node{ID: 10}, model.Node{ID: 20}, model.NodeGroup{ID: 30, Revision: 1}, model.ManagedCertificate{ID: 40}
}

func protocolEndpointMutationRequest(nodeID uint) network.ProtocolEndpointMutationRequest {
	active := true
	return network.ProtocolEndpointMutationRequest{
		NodeID: nodeID, Name: "endpoint", Protocol: "vless", Address: "edge.example.test", Port: 443,
		PublicPort: 443, MultiplierMilli: 1000, IsActive: &active,
		ServerConfig: `{"type":"vless","users":[]}`, ClientConfig: `{"type":"vless"}`, OptionalConfig: "{}", Tags: "[]",
		CredentialProtocols: []string{"vless"},
	}
}

func requireProtocolEndpointMutationField(t *testing.T, err error, field string) {
	t.Helper()
	var validation *network.ProtocolEndpointMutationValidation
	if !errors.As(err, &validation) || validation.Fields[field] == "" {
		t.Fatalf("error=%v fields=%v want field=%q", err, validation, field)
	}
}

func TestProtocolEndpointMutationsCommitEndpointMembershipCertificateTaskAndAudit(t *testing.T) {
	db, _ := administrationFixture(t)
	node, _, group, certificate := seedProtocolEndpointMutationAuthority(t, db)
	service := protocolEndpointMutationService(db)
	request := protocolEndpointMutationRequest(node.ID)
	request.ManagedCertificateID = &certificate.ID
	request.MembershipChanges = []network.ProtocolEndpointMembershipChange{{NodeGroupID: group.ID, ExpectedRevision: group.Revision, Member: true}}
	result, err := service.Save(context.Background(), 1, nil, request)
	if err != nil || result.ProtocolEndpoint.ID == 0 || result.ProtocolEndpoint.SortOrder != 0 || result.MembershipMutation == nil {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if len(result.Memberships) != 1 || result.Memberships[0].NodeGroupID != group.ID || len(result.MembershipMutation.AddedNodeGroupIDs) != 1 || len(result.MembershipMutation.ReconcileTasks) != 1 {
		t.Fatalf("membership result=%+v", result)
	}
	if result.MembershipMutation.ReconcileTasks[0].Total != 2 || result.MembershipMutation.PublishStatus != network.ProtocolEndpointPublishQueued {
		t.Fatalf("reconcile=%+v", result.MembershipMutation)
	}
	var endpoint model.ProtocolEndpoint
	if err := db.First(&endpoint, result.ProtocolEndpoint.ID).Error; err != nil || endpoint.ServerConfig != `enc:{"type":"vless","users":[]}` || endpoint.RuntimeKey == "" {
		t.Fatalf("endpoint=%+v error=%v", endpoint, err)
	}
	var link model.CertificateProtocolEndpoint
	if err := db.Where("protocol_endpoint_id = ?", endpoint.ID).First(&link).Error; err != nil || link.ManagedCertificateID != certificate.ID {
		t.Fatalf("certificate link=%+v error=%v", link, err)
	}
	if err := db.First(&group, group.ID).Error; err != nil || group.Revision != 2 {
		t.Fatalf("group=%+v error=%v", group, err)
	}
	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("action IN ?", []string{"protocol_endpoint.create", "node_group.membership.update", "task.create"}).Count(&audits).Error; err != nil || audits != 3 {
		t.Fatalf("audits=%d error=%v", audits, err)
	}
	var publications int64
	if err := db.Model(&model.NodeConfigPublish{}).Count(&publications).Error; err != nil || publications != 0 {
		t.Fatalf("direct publication must wait for reconcile: count=%d error=%v", publications, err)
	}
}

func TestProtocolEndpointMutationsMoveCredentialsAndQueueBothNodes(t *testing.T) {
	db, _ := administrationFixture(t)
	nodeA, nodeB, _, _ := seedProtocolEndpointMutationAuthority(t, db)
	service := protocolEndpointMutationService(db)
	created, err := service.Save(context.Background(), 1, nil, protocolEndpointMutationRequest(nodeA.ID))
	if err != nil {
		t.Fatal(err)
	}
	credential := model.ProtocolCredential{ID: 70, SubscriptionID: 71, UserID: 2, ProtocolEndpointID: created.ProtocolEndpoint.ID, NodeID: nodeA.ID, CredentialID: "credential-70", PrincipalKey: "principal-70", Secret: "enc:secret", ListenPort: 443, PublicPort: 443, Status: "active", ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("1 = 1").Delete(&model.NodeConfigPublish{}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Load(context.Background(), 1, created.ProtocolEndpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := protocolEndpointMutationRequest(nodeB.ID)
	request.ID, request.Port, request.PublicPort = created.ProtocolEndpoint.ID, 8443, 9443
	updated, err := service.Save(context.Background(), 1, &snapshot, request)
	if err != nil || updated.ProtocolEndpoint.NodeID != nodeB.ID || updated.Effect != network.ProtocolEndpointEffectCredentialPlacement {
		t.Fatalf("updated=%+v error=%v", updated, err)
	}
	if err := db.First(&credential, credential.ID).Error; err != nil || credential.NodeID != nodeB.ID || credential.ListenPort != 8443 || credential.PublicPort != 9443 {
		t.Fatalf("credential=%+v error=%v", credential, err)
	}
	var publications []model.NodeConfigPublish
	if err := db.Order("node_id").Find(&publications).Error; err != nil || len(publications) != 2 || publications[0].NodeID != nodeA.ID || publications[1].NodeID != nodeB.ID {
		t.Fatalf("publications=%+v error=%v", publications, err)
	}
}

func TestProtocolEndpointMutationsRejectStalePermissionGroupConflictAndRollback(t *testing.T) {
	for _, scenario := range []string{"stale", "permission", "group", "deleting", "audit"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			node, _, group, _ := seedProtocolEndpointMutationAuthority(t, db)
			service := protocolEndpointMutationService(db)
			created, err := service.Save(context.Background(), 1, nil, protocolEndpointMutationRequest(node.ID))
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Where("1 = 1").Delete(&model.NodeConfigPublish{}).Error; err != nil {
				t.Fatal(err)
			}
			snapshot, err := service.Load(context.Background(), 1, created.ProtocolEndpoint.ID)
			if err != nil {
				t.Fatal(err)
			}
			request := protocolEndpointMutationRequest(node.ID)
			request.ID, request.Name = created.ProtocolEndpoint.ID, "blocked"
			switch scenario {
			case "stale":
				if err := db.Model(&model.ProtocolEndpoint{}).Where("id = ?", request.ID).Update("name", "concurrent").Error; err != nil {
					t.Fatal(err)
				}
			case "permission":
				if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
					t.Fatal(err)
				}
			case "group":
				request.MembershipChanges = []network.ProtocolEndpointMembershipChange{{NodeGroupID: group.ID, ExpectedRevision: group.Revision + 1, Member: true}}
			case "deleting":
				if err := db.Model(&model.Node{}).Where("id = ?", node.ID).Update("lifecycle_status", "deleting").Error; err != nil {
					t.Fatal(err)
				}
			case "audit":
				if err := db.Callback().Create().Before("gorm:create").Register("fail_protocol_endpoint_mutation_audit", func(tx *gorm.DB) {
					if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "protocol_endpoint.update" {
						tx.AddError(errors.New("audit unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Create().Remove("fail_protocol_endpoint_mutation_audit")
			}
			_, err = service.Save(context.Background(), 1, &snapshot, request)
			switch scenario {
			case "stale":
				if !errors.Is(err, network.ErrProtocolEndpointConflict) {
					t.Fatalf("error=%v", err)
				}
			case "permission":
				if !errors.Is(err, network.ErrProtocolEndpointMutationPermission) {
					t.Fatalf("error=%v", err)
				}
			case "group":
				var conflict *network.ProtocolEndpointMembershipConflictError
				if !errors.As(err, &conflict) {
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
			var endpoint model.ProtocolEndpoint
			if err := db.First(&endpoint, request.ID).Error; err != nil {
				t.Fatal(err)
			}
			wantName := "endpoint"
			if scenario == "stale" {
				wantName = "concurrent"
			}
			if endpoint.Name != wantName {
				t.Fatalf("endpoint name=%q want=%q", endpoint.Name, wantName)
			}
			var publications int64
			if err := db.Model(&model.NodeConfigPublish{}).Count(&publications).Error; err != nil || publications != 0 {
				t.Fatalf("partial publications=%d error=%v", publications, err)
			}
		})
	}
}

func TestProtocolEndpointMutationsRejectMissingNodeAndTopologyConflicts(t *testing.T) {
	t.Run("missing node", func(t *testing.T) {
		db, _ := administrationFixture(t)
		seedProtocolEndpointMutationAuthority(t, db)
		_, err := protocolEndpointMutationService(db).Save(context.Background(), 1, nil, protocolEndpointMutationRequest(999))
		requireProtocolEndpointMutationField(t, err, "node_id")
		var count int64
		if err := db.Model(&model.ProtocolEndpoint{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("endpoints=%d error=%v", count, err)
		}
	})

	t.Run("landing node conflicts with existing entry", func(t *testing.T) {
		db, _ := administrationFixture(t)
		nodeA, nodeB, _, _ := seedProtocolEndpointMutationAuthority(t, db)
		service := protocolEndpointMutationService(db)
		created, err := service.Save(context.Background(), 1, nil, protocolEndpointMutationRequest(nodeA.ID))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.NetworkEntry{ID: 81, NodeID: nodeB.ID, EndpointID: created.ProtocolEndpoint.ID, Name: "front", Address: "front.example.test", Port: 10081, PublicPort: 10081, Network: "tcp_udp", Enabled: true, Revision: 1}).Error; err != nil {
			t.Fatal(err)
		}
		snapshot, err := service.Load(context.Background(), 1, created.ProtocolEndpoint.ID)
		if err != nil {
			t.Fatal(err)
		}
		request := protocolEndpointMutationRequest(nodeB.ID)
		request.ID = created.ProtocolEndpoint.ID
		_, err = service.Save(context.Background(), 1, &snapshot, request)
		requireProtocolEndpointMutationField(t, err, "node_id")
	})

	t.Run("listen port conflicts with entry", func(t *testing.T) {
		db, _ := administrationFixture(t)
		nodeA, nodeB, _, _ := seedProtocolEndpointMutationAuthority(t, db)
		service := protocolEndpointMutationService(db)
		landing, err := service.Save(context.Background(), 1, nil, protocolEndpointMutationRequest(nodeB.ID))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.NetworkEntry{ID: 82, NodeID: nodeA.ID, EndpointID: landing.ProtocolEndpoint.ID, Name: "occupied", Address: "front.example.test", Port: 10443, PublicPort: 10443, Network: "tcp_udp", Enabled: true, Revision: 1}).Error; err != nil {
			t.Fatal(err)
		}
		request := protocolEndpointMutationRequest(nodeA.ID)
		request.Port, request.PublicPort = 10443, 10443
		_, err = service.Save(context.Background(), 1, nil, request)
		requireProtocolEndpointMutationField(t, err, "port")
	})
}

func TestProtocolEndpointMutationsRejectInvalidCertificateState(t *testing.T) {
	for _, scenario := range []string{"wrong node", "expired", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			nodeA, nodeB, _, certificate := seedProtocolEndpointMutationAuthority(t, db)
			request := protocolEndpointMutationRequest(nodeA.ID)
			request.ManagedCertificateID = &certificate.ID
			switch scenario {
			case "wrong node":
				request.NodeID = nodeB.ID
			case "expired":
				expired := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
				if err := db.Model(&model.ManagedCertificate{}).Where("id = ?", certificate.ID).Update("not_after", expired).Error; err != nil {
					t.Fatal(err)
				}
			case "deleting":
				if err := db.Model(&model.ManagedCertificate{}).Where("id = ?", certificate.ID).Update("status", "deleting").Error; err != nil {
					t.Fatal(err)
				}
			}
			_, err := protocolEndpointMutationService(db).Save(context.Background(), 1, nil, request)
			if scenario == "deleting" {
				if !errors.Is(err, network.ErrProtocolEndpointResourceDeleting) {
					t.Fatalf("error=%v", err)
				}
			} else {
				requireProtocolEndpointMutationField(t, err, "managed_certificate_id")
			}
		})
	}
}

func TestProtocolEndpointMutationsRejectInvalidParentChain(t *testing.T) {
	t.Run("different node", func(t *testing.T) {
		db, _ := administrationFixture(t)
		nodeA, nodeB, _, _ := seedProtocolEndpointMutationAuthority(t, db)
		service := protocolEndpointMutationService(db)
		parent, err := service.Save(context.Background(), 1, nil, protocolEndpointMutationRequest(nodeA.ID))
		if err != nil {
			t.Fatal(err)
		}
		request := protocolEndpointMutationRequest(nodeB.ID)
		request.Name, request.ParentProtocolID = "child", &parent.ProtocolEndpoint.ID
		_, err = service.Save(context.Background(), 1, nil, request)
		requireProtocolEndpointMutationField(t, err, "parent_protocol_id")
	})

	t.Run("cycle", func(t *testing.T) {
		db, _ := administrationFixture(t)
		node, _, _, _ := seedProtocolEndpointMutationAuthority(t, db)
		service := protocolEndpointMutationService(db)
		parent, err := service.Save(context.Background(), 1, nil, protocolEndpointMutationRequest(node.ID))
		if err != nil {
			t.Fatal(err)
		}
		childRequest := protocolEndpointMutationRequest(node.ID)
		childRequest.Name, childRequest.ParentProtocolID = "child", &parent.ProtocolEndpoint.ID
		child, err := service.Save(context.Background(), 1, nil, childRequest)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := service.Load(context.Background(), 1, parent.ProtocolEndpoint.ID)
		if err != nil {
			t.Fatal(err)
		}
		request := protocolEndpointMutationRequest(node.ID)
		request.ID, request.ParentProtocolID = parent.ProtocolEndpoint.ID, &child.ProtocolEndpoint.ID
		_, err = service.Save(context.Background(), 1, &snapshot, request)
		requireProtocolEndpointMutationField(t, err, "parent_protocol_id")
	})
}

func TestProtocolEndpointMutationsRejectCredentialBreakingChanges(t *testing.T) {
	for _, scenario := range []string{"protocol", "shadowsocks port"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			node, _, _, _ := seedProtocolEndpointMutationAuthority(t, db)
			service := protocolEndpointMutationService(db)
			request := protocolEndpointMutationRequest(node.ID)
			if scenario == "shadowsocks port" {
				request.Protocol = "shadowsocks"
			}
			created, err := service.Save(context.Background(), 1, nil, request)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ProtocolCredential{ID: 90, SubscriptionID: 91, UserID: 2, ProtocolEndpointID: created.ProtocolEndpoint.ID, NodeID: node.ID, CredentialID: "credential-90", PrincipalKey: "principal-90", Secret: "enc:secret", ListenPort: 443, PublicPort: 443, Status: "active", ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}).Error; err != nil {
				t.Fatal(err)
			}
			snapshot, err := service.Load(context.Background(), 1, created.ProtocolEndpoint.ID)
			if err != nil {
				t.Fatal(err)
			}
			request.ID = created.ProtocolEndpoint.ID
			if scenario == "protocol" {
				request.Protocol = "vmess"
			} else {
				request.Port, request.PublicPort = 8443, 8443
			}
			_, err = service.Save(context.Background(), 1, &snapshot, request)
			requireProtocolEndpointMutationField(t, err, "protocol")
			var endpoint model.ProtocolEndpoint
			if err := db.First(&endpoint, created.ProtocolEndpoint.ID).Error; err != nil || endpoint.Protocol != created.ProtocolEndpoint.Protocol || endpoint.Port != 443 {
				t.Fatalf("endpoint=%+v error=%v", endpoint, err)
			}
		})
	}
}
