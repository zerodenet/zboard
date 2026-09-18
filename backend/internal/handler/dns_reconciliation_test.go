package handler

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
	"testing"
)

func TestDNSObservationOnlyReadsStoredProviderIdentity(t *testing.T) {
	f, _, account := deletionFixture(t)
	calls := 0
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/zones/zone-1/dns_records/remote-1" {
			t.Errorf("unexpected provider operation %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		fmt.Fprint(w, `{"success":true,"result":{"id":"remote-1","type":"A","name":"example.test","content":"192.0.2.1","ttl":1,"proxied":false}}`)
	})
	scope := network.DNSReconciliationScope{AccountID: account.ID, AccountRevision: account.Revision, ZoneID: "zone-1", RemoteID: "remote-1"}
	result, err := f.h.ObserveDNS(context.Background(), scope)
	if err != nil || result.RemoteID != "remote-1" || result.Hash != network.DNSRecordHash("A", "example.test", "192.0.2.1", 1, false) {
		t.Fatal(result, err)
	}
	scope.RemoteID = ""
	if _, err = f.h.ObserveDNS(context.Background(), scope); err == nil || calls != 1 {
		t.Fatal("missing identity contacted provider", calls, err)
	}
	scope.RemoteID = "remote-1"
	scope.AccountRevision++
	if _, err = f.h.ObserveDNS(context.Background(), scope); err == nil || calls != 1 {
		t.Fatal("changed account contacted provider", calls, err)
	}
}
