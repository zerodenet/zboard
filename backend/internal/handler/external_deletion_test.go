package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func deletionFixture(t *testing.T) (trafficReadFixture, model.Node, model.ProviderAccount) {
	t.Helper()
	f := newTrafficReadFixture(t)
	node := model.Node{Name: "deletion-fixture", Address: "192.0.2.1", Config: "{}"}
	secret, err := f.h.credentialCipher.Encrypt("fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	account := model.ProviderAccount{Name: "fixture", ProviderKey: providerCloudflare, Capabilities: "[]", CredentialCiphertext: secret, Status: "active"}
	for _, value := range []interface{}{&node, &account} {
		if err := f.h.db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	return f, node, account
}

func seedDeletionDNS(t *testing.T, f trafficReadFixture, node model.Node, account model.ProviderAccount, remote string) model.ManagedDNSRecord {
	t.Helper()
	record := model.ManagedDNSRecord{NodeID: node.ID, ProviderAccountID: account.ID, DomainName: remote + ".example.test", RecordType: "A", RecordValue: "192.0.2.1", ProviderZoneID: "zone-1", ProviderRecordID: remote, Status: dnsStatusActive, DesiredHash: remote}
	if err := f.h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	return record
}

func mockDeletionDNS(t *testing.T, fn http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(fn)
	previous := cloudflareAPIBaseURL
	cloudflareAPIBaseURL = server.URL
	t.Cleanup(func() { cloudflareAPIBaseURL = previous; server.Close() })
}

func TestDNSDeletionRemovesLocalRecordWithoutUsingExpiredProviderCredentials(t *testing.T) {
	f, node, account := deletionFixture(t)
	record := seedDeletionDNS(t, f, node, account, "record")
	calls := 0
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(403) })
	w := httptest.NewRecorder()
	f.h.ManagedDNSDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/admin/dns-records/%d", record.ID), f.admin, ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remote_record_deleted":false`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var count int64
	f.h.db.Model(&model.ManagedDNSRecord{}).Where("id = ?", record.ID).Count(&count)
	if count != 0 || calls != 0 {
		t.Fatalf("local=%d remote calls=%d", count, calls)
	}
	if _, err := f.h.startDNSOperation(record.ID, false, nil); err == nil {
		t.Fatal("deleted DNS could be synchronized")
	}
}

func TestNodeDeletionClearsRelationsWithoutSSHOrProviderAccess(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, b, "")
	group := model.NodeGroup{Name: "assigned", Code: "assigned"}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: b.ID}).Error; err != nil {
		t.Fatal(err)
	}
	pool := model.NodeProxyPool{NodeID: b.NodeID, Name: "owned", Config: "encrypted"}
	if err := f.h.db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	f.h.db.Model(&model.NodeKernelState{}).Where("node_id = ?", b.NodeID).Update("installed_version", "0.0.1")
	now := time.Now().UTC()
	f.h.db.Model(&model.Node{}).Where("id = ?", b.NodeID).Updates(map[string]interface{}{"connector_last_seen_at": now, "ssh_host": "", "ssh_user": ""})
	calls := 0
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(403) })
	f.h.db.Where("1 = 1").Delete(&model.NodeConfigPublish{})
	w := httptest.NewRecorder()
	f.h.NodeCascadeDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/nodes/%d", b.NodeID), f.admin, ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remote_zero_stopped":false`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, query := range []*gorm.DB{
		f.h.db.Model(&model.Node{}).Where("id = ?", b.NodeID),
		f.h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", b.ID),
		f.h.db.Model(&model.NetworkEntry{}).Where("id = ?", entry.ID),
		f.h.db.Model(&model.NodeGroupNetworkEntry{}).Where("node_group_id = ?", group.ID),
		f.h.db.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", group.ID),
		f.h.db.Model(&model.NodeProxyPool{}).Where("node_id = ?", b.NodeID),
	} {
		var count int64
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("local cleanup count=%d err=%v", count, err)
		}
	}
	var surviving model.Node
	if err := f.h.db.First(&surviving, a.ID).Error; err != nil {
		t.Fatal("entry node was removed")
	}
	var queue model.NodeConfigPublish
	if err := f.h.db.Where("node_id = ?", a.ID).First(&queue).Error; err != nil {
		t.Fatal("surviving entry withdrawal not queued")
	}
	if calls != 0 {
		t.Fatal("local deletion contacted provider")
	}
}

func TestProviderDeletionClearsLocalLinksWithoutRemovingCertificates(t *testing.T) {
	f, node, account := deletionFixture(t)
	cert := model.ManagedCertificate{NodeID: node.ID, ProviderAccountID: &account.ID, Name: "fixture", Domains: `["fixture.example.test"]`, ContactEmail: "admin@example.test", AutoRenew: true}
	if err := f.h.db.Create(&cert).Error; err != nil {
		t.Fatal(err)
	}
	record := seedDeletionDNS(t, f, node, account, "record")
	w := httptest.NewRecorder()
	f.h.ProviderAccountDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/admin/provider-accounts/%d", account.ID), f.admin, ""))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var retained model.ManagedCertificate
	if err := f.h.db.First(&retained, cert.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.ProviderAccountID != nil || retained.AutoRenew {
		t.Fatal("deleted provider still bound to certificate")
	}
	var count int64
	f.h.db.Model(&model.ManagedDNSRecord{}).Where("id = ?", record.ID).Count(&count)
	if count != 0 {
		t.Fatal("DNS link remains")
	}
}

func TestCertificateDeletionRemovesExpiredRecordAndLinksWithoutSSH(t *testing.T) {
	f, node, _ := deletionFixture(t)
	expiry := time.Now().Add(-time.Hour)
	cert := model.ManagedCertificate{NodeID: node.ID, Name: "fixture", Domains: `["fixture.example.test"]`, ContactEmail: "admin@example.test", Status: certificateStatusActive, CertPath: "/etc/zboard/certificates/1/current/fullchain.pem", KeyPath: "/etc/zboard/certificates/1/current/privkey.pem", NotAfter: &expiry}
	if err := f.h.db.Create(&cert).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "tls", Protocol: "trojan", Port: 443}
	if err := f.h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.CertificateProtocolEndpoint{ManagedCertificateID: cert.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.ManagedCertificateDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/admin/certificates/%d", cert.ID), f.admin, ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remote_files_retained":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if _, err := f.h.startManagedCertificateOperation(cert.ID, certificateOperationRenew, nil); err == nil {
		t.Fatal("deleted certificate renewed")
	}
	var count int64
	f.h.db.Model(&model.CertificateProtocolEndpoint{}).Where("managed_certificate_id = ?", cert.ID).Count(&count)
	if count != 0 {
		t.Fatal("certificate link remains")
	}
	if err := f.h.db.First(&endpoint, endpoint.ID).Error; err != nil {
		t.Fatal("certificate deletion removed protocol")
	}
}

func TestDNSDeletionRejectsIncompleteOrUncertainOwnership(t *testing.T) {
	f, node, account := deletionFixture(t)
	record := seedDeletionDNS(t, f, node, account, "record")
	record.ProviderZoneID = ""
	if _, err := f.h.deleteManagedDNSRemote(t.Context(), record); err == nil {
		t.Fatal("incomplete identity accepted")
	}
	record.ProviderRecordID = ""
	operation := model.ProviderOperation{ProviderAccountID: account.ID, ResourceType: "dns_record", ResourceID: record.ID, OperationType: "sync", Status: "failed", Phase: "persisting"}
	if err := f.h.db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.deleteManagedDNSRemote(t.Context(), record); err == nil {
		t.Fatal("uncertain remote write silently forgotten")
	}
}
