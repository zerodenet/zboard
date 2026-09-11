package handler

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProxyPoolDefaultAgentSelectsSupportedSubscriptionFormat(t *testing.T) {
	selection := resolveSubscriptionDelivery("", proxyPoolSubscriptionDefaultAgent)
	if selection.Format != "clash" {
		t.Fatalf("default agent selects unsupported format: %#v", selection)
	}
}

func TestDueProxyPoolSubscriptionReplacesMembersAndQueuesPublish(t *testing.T) {
	poolValidationForTest(t)
	f, node, _ := networkEntryFixture(t)
	pool := createPoolForTest(t, f, node.ID)
	encrypted, err := f.h.credentialCipher.Encrypt("https://example.com/subscription/token")
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(-time.Minute)
	if err := f.h.db.Model(&pool).Updates(map[string]interface{}{"subscription_url": encrypted, "subscription_format": "zero", "auto_sync": true, "sync_interval_seconds": 3600, "next_sync_at": due}).Error; err != nil {
		t.Fatal(err)
	}
	raw := `{"outbounds":[{"tag":"replacement","protocol":{"type":"shadowsocks","server":"198.51.100.1","port":443,"cipher":"aes-128-gcm","password":"updated"}}],"target":"replacement"}`
	calls := 0
	previous := proxyPoolSubscriptionFetcher
	t.Cleanup(func() { proxyPoolSubscriptionFetcher = previous })
	proxyPoolSubscriptionFetcher = func(context.Context, string, string) ([]byte, error) {
		calls++
		return []byte(base64.StdEncoding.EncodeToString([]byte(raw))), nil
	}
	now := time.Now().UTC()
	f.h.syncDueProxyPools(context.Background(), now)
	var updated model.NodeProxyPool
	if err := f.h.db.First(&updated, pool.ID).Error; err != nil {
		t.Fatal(err)
	}
	config, err := f.h.credentialCipher.Decrypt(updated.Config)
	if err != nil {
		t.Fatal(err)
	}
	path, count, err := parseProxyPoolSubscription([]byte(config), "zero-json")
	if err != nil || count != 1 || path.Target != "replacement" || updated.Revision != pool.Revision+1 || updated.LastSyncAt == nil || updated.NextSyncAt == nil || !updated.NextSyncAt.After(now) {
		t.Fatalf("sync did not replace and schedule pool: count=%d target=%s revision=%d err=%v", count, path.Target, updated.Revision, err)
	}
	var publishes []model.NodeConfigPublish
	if err := f.h.db.Where("node_id = ?", node.ID).Find(&publishes).Error; err != nil {
		t.Fatal(err)
	}
	if len(publishes) == 0 {
		t.Fatal("successful sync did not queue publication")
	}
	f.h.syncDueProxyPools(context.Background(), now)
	if calls != 1 {
		t.Fatalf("not-due subscription fetched again: %d", calls)
	}
}

func TestProxyPoolSingleOutboundCompilesWithoutNullGroups(t *testing.T) {
	path := networkEntryPath{Outbounds: []map[string]interface{}{{"tag": "only", "protocol": map[string]interface{}{"type": "direct"}}}, Target: "only"}
	config := map[string]interface{}{}
	if _, err := path.appendGraph(config, "pool/"); err != nil {
		t.Fatal(err)
	}
	groups, ok := config["outbound_groups"].([]interface{})
	if !ok || groups == nil || len(groups) != 0 {
		t.Fatal("missing groups must compile to an empty array")
	}
}
