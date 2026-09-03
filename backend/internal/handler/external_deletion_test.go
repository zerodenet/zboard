package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
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

func TestDNSDeletionFailureRetainsIdentityAndRetryAfterRestart(t *testing.T) {
	f, node, account := deletionFixture(t)
	record := seedDeletionDNS(t, f, node, account, "record-1")
	failure := true
	calls := 0
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodDelete || r.URL.Path != "/zones/zone-1/dns_records/record-1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if failure {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"success":false,"errors":[{"message":"denied"}]}`)
			return
		}
		// The first attempt may have completed remotely despite a lost response.
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"success":false,"errors":[{"message":"absent"}]}`)
	})
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.h.ManagedDNSDeleteHandler(w, announcementRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/dns-records/%d", record.ID), f.admin, ""))
		return w
	}
	if w := request(); w.Code != http.StatusBadGateway {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	var retained model.ManagedDNSRecord
	if err := f.h.db.First(&retained, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.Status != resourceStatusDeleting || retained.ProviderRecordID != record.ProviderRecordID || retained.LastError == "" {
		t.Fatalf("lost recovery state: %+v", retained)
	}
	h, err := NewHandlers(f.h.db, f.h.jwtSecret, f.h.credentialCipher, "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	f.h = h
	if _, err := f.h.startDNSOperation(record.ID, false, nil); err == nil {
		t.Fatal("sync resurrected deleting resource")
	}
	failure = false
	if w := request(); w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	var count int64
	f.h.db.Model(&model.ManagedDNSRecord{}).Where("id = ?", record.ID).Count(&count)
	if count != 0 || calls != 2 {
		t.Fatalf("count=%d calls=%d", count, calls)
	}
}

func TestNodeDeletionRetriesPartialDNSCleanupBeforeDroppingRows(t *testing.T) {
	f, node, account := deletionFixture(t)
	seedDeletionDNS(t, f, node, account, "first")
	seedDeletionDNS(t, f, node, account, "second")
	failSecond := true
	removed := map[string]bool{}
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/second") && failSecond {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"success":false,"errors":[{"message":"denied"}]}`)
			return
		}
		if removed[r.URL.Path] {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"success":false,"errors":[{"message":"absent"}]}`)
			return
		}
		removed[r.URL.Path] = true
		fmt.Fprint(w, `{"success":true,"result":{}}`)
	})
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.h.NodeCascadeDeleteHandler(w, announcementRequest(http.MethodDelete, fmt.Sprintf("/api/v1/nodes/%d", node.ID), f.admin, ""))
		return w
	}
	if w := request(); w.Code != 502 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	var count int64
	f.h.db.Model(&model.ManagedDNSRecord{}).Where("node_id = ?", node.ID).Count(&count)
	if count != 2 {
		t.Fatalf("partial cleanup lost local identities: %d", count)
	}
	var retained model.Node
	if err := f.h.db.First(&retained, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.LifecycleStatus != resourceStatusDeleting || retained.IsEnabled {
		t.Fatal("node deletion intent lost")
	}
	if err := requireAvailableNode(f.h.db, node.ID); err == nil {
		t.Fatal("new attachments accepted during deletion")
	}
	failSecond = false
	if w := request(); w.Code != 200 {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	f.h.db.Model(&model.Node{}).Where("id = ?", node.ID).Count(&count)
	if count != 0 {
		t.Fatal("node remains")
	}
	f.h.db.Model(&model.ManagedDNSRecord{}).Where("node_id = ?", node.ID).Count(&count)
	if count != 0 || len(removed) != 2 {
		t.Fatal("external/local cleanup incomplete")
	}
}

func TestProviderDeletionRequiresBothDNSAndCertificateCleanup(t *testing.T) {
	f, node, account := deletionFixture(t)
	cert := model.ManagedCertificate{NodeID: node.ID, ProviderAccountID: &account.ID, Name: "fixture", Domains: `["fixture.example.test"]`, ContactEmail: "admin@example.test"}
	if err := f.h.db.Create(&cert).Error; err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.h.ProviderAccountDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/admin/provider-accounts/%d", account.ID), f.admin, ""))
		return w
	}
	if w := request(); w.Code != 409 || !strings.Contains(w.Body.String(), "certificates") {
		t.Fatalf("certificate blocker: %s", w.Body.String())
	}
	f.h.db.Delete(&cert)
	record := seedDeletionDNS(t, f, node, account, "record")
	if w := request(); w.Code != 409 || !strings.Contains(w.Body.String(), "dns_records") {
		t.Fatal("DNS blocker missing")
	}
	f.h.db.Delete(&record)
	if w := request(); w.Code != 200 {
		t.Fatalf("unreferenced account: %s", w.Body.String())
	}
}

func TestCertificateDeletionFailureBlocksRenewalAndRetainsMetadata(t *testing.T) {
	f, node, _ := deletionFixture(t)
	expiry := time.Now().Add(-time.Hour)
	cert := model.ManagedCertificate{NodeID: node.ID, Name: "fixture", Domains: `["fixture.example.test"]`, ContactEmail: "admin@example.test", Status: certificateStatusActive, CertPath: "/etc/zboard/certificates/1/current/fullchain.pem", KeyPath: "/etc/zboard/certificates/1/current/privkey.pem", NotAfter: &expiry}
	if err := f.h.db.Create(&cert).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.ManagedCertificateDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/admin/certificates/%d", cert.ID), f.admin, ""))
	if w.Code != 502 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if _, err := f.h.startManagedCertificateOperation(cert.ID, certificateOperationRenew, nil); err == nil {
		t.Fatal("renewal restarted deletion")
	}
	f.h.scanCertificateRenewals(time.Now())
	var retained model.ManagedCertificate
	if err := f.h.db.First(&retained, cert.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.Status != resourceStatusDeleting || retained.AutoRenew || retained.LastError == "" {
		t.Fatalf("lost deletion intent: %+v", retained)
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
