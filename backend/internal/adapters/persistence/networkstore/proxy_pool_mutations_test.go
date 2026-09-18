package networkstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type proxyPoolCipher struct{}

func (proxyPoolCipher) Encrypt(value string) (string, error) { return "enc:" + value, nil }
func (proxyPoolCipher) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(value, "enc:"), nil
}

type proxyPoolInspectorFunc func(context.Context, string, bool) (network.ProxyPoolConfigurationFacts, error)

func (f proxyPoolInspectorFunc) InspectProxyPoolConfiguration(ctx context.Context, raw string, validateNative bool) (network.ProxyPoolConfigurationFacts, error) {
	return f(ctx, raw, validateNative)
}

func proxyPoolMutationService(db *gorm.DB) network.ProxyPoolMutations {
	return network.ProxyPoolMutations{
		Repository: ProxyPoolMutations{DB: db}, Cipher: proxyPoolCipher{},
		Inspector: proxyPoolInspectorFunc(func(_ context.Context, raw string, _ bool) (network.ProxyPoolConfigurationFacts, error) {
			if raw == "tcp-only" {
				return network.ProxyPoolConfigurationFacts{DatagramError: "HTTP CONNECT does not support UDP"}, nil
			}
			return network.ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
		}),
	}
}

func TestProxyPoolMutationsCommitSecretsConstraintsAuditAndPublicationAtomically(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	service := proxyPoolMutationService(db)
	config, subscriptionURL, auto := "udp-capable", "https://subscription.example.test/list", true
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	created, err := service.Save(context.Background(), 1, network.ProxyPoolMutationRequest{
		NodeID: 1, Name: "shared", Config: &config, SubscriptionURL: &subscriptionURL,
		SubscriptionFormat: "zero", SubscriptionUserAgent: "zboard-test", AutoSync: &auto,
		SyncIntervalSeconds: 600, InitialSyncAt: &now, InitialNodeCount: 2,
	})
	if err != nil || created.ID == 0 || created.Revision != 1 || !created.AutoSync || created.SubscriptionNodeCount != 2 {
		t.Fatalf("created=%+v error=%v", created, err)
	}
	var stored model.NodeProxyPool
	if err := db.First(&stored, created.ID).Error; err != nil || stored.Config != "enc:udp-capable" || stored.SubscriptionURL != "enc:https://subscription.example.test/list" {
		t.Fatalf("stored=%+v error=%v", stored, err)
	}
	var publish model.NodeConfigPublish
	if err := db.First(&publish, "node_id = ?", 1).Error; err != nil || publish.RequestedBy != 1 || publish.Generation != 1 {
		t.Fatalf("publish=%+v error=%v", publish, err)
	}
	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "node_proxy_pool.update").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("audits=%d error=%v", audits, err)
	}

	endpoint := model.ProtocolEndpoint{NodeID: 1, Name: "landing", RuntimeKey: "pool-mutation-landing", Protocol: "vless", Port: 443, PublicPort: 443, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	poolID := created.ID
	entry := model.NetworkEntry{Name: "udp-entry", NodeID: 1, EndpointID: endpoint.ID, Network: "tcp_udp", Port: 1443, PublicPort: 1443, Enabled: true, ProxyPoolID: &poolID}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	tcpOnly := "tcp-only"
	if _, err := service.Save(context.Background(), 1, network.ProxyPoolMutationRequest{ID: created.ID, NodeID: 1, Name: "incompatible", ExpectedRevision: created.Revision, Config: &tcpOnly}); err == nil {
		t.Fatal("datagram-incompatible update accepted")
	}
	if err := db.First(&stored, created.ID).Error; err != nil || stored.Name != "shared" || stored.Revision != 1 || stored.Config != "enc:udp-capable" {
		t.Fatalf("failed update escaped rollback: stored=%+v error=%v", stored, err)
	}
	if _, err := service.Save(context.Background(), 1, network.ProxyPoolMutationRequest{ID: created.ID, Delete: true}); err == nil {
		t.Fatal("referenced pool deleted")
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(context.Background(), 1, network.ProxyPoolMutationRequest{ID: created.ID, NodeID: 1, Name: "denied", ExpectedRevision: created.Revision}); !errors.Is(err, network.ErrProxyPoolMutationPermission) {
		t.Fatalf("revoked authority error=%v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(context.Background(), 1, network.ProxyPoolMutationRequest{ID: created.ID, Delete: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&stored, created.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted pool remains: %+v error=%v", stored, err)
	}
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "node_proxy_pool.update").Count(&audits).Error; err != nil || audits != 2 {
		t.Fatalf("audits=%d error=%v", audits, err)
	}
	if err := db.First(&publish, "node_id = ?", 1).Error; err != nil || publish.Generation != 2 {
		t.Fatalf("publish=%+v error=%v", publish, err)
	}
}

func TestProxyPoolMutationAuditFailureRollsBackPoolAndPublication(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("fail_proxy_pool_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("fail_proxy_pool_audit") })
	config := "udp-capable"
	if _, err := proxyPoolMutationService(db).Save(context.Background(), 1, network.ProxyPoolMutationRequest{NodeID: 1, Name: "must-rollback", Config: &config}); err == nil {
		t.Fatal("audit failure accepted")
	}
	for name, value := range map[string]any{"node_proxy_pools": &model.NodeProxyPool{}, "node_config_publishes": &model.NodeConfigPublish{}} {
		var count int64
		if err := db.Model(value).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d error=%v", name, count, err)
		}
	}
}
