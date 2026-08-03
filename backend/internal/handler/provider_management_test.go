package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDNSRecordHashIsStableAndSensitive(t *testing.T) {
	first := dnsRecordHash("a", "EDGE.Example.com", "203.0.113.10", 120, false)
	second := dnsRecordHash("A", "edge.example.com", "203.0.113.10", 120, false)
	if first != second {
		t.Fatalf("normalized hashes differ: %s != %s", first, second)
	}
	if first == dnsRecordHash("A", "edge.example.com", "203.0.113.11", 120, false) {
		t.Fatal("record value change did not affect desired hash")
	}
}

func TestFindCloudflareZoneUsesLongestVisibleSuffix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("name") == "example.com" {
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"zone-1","name":"example.com"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[]}`))
	}))
	defer server.Close()
	previous := cloudflareAPIBaseURL
	cloudflareAPIBaseURL = server.URL
	defer func() { cloudflareAPIBaseURL = previous }()

	zone, err := findCloudflareZone(context.Background(), "token", "edge.hk.example.com")
	if err != nil {
		t.Fatalf("findCloudflareZone() error = %v", err)
	}
	if zone.ID != "zone-1" || zone.Name != "example.com" {
		t.Fatalf("findCloudflareZone() = %#v", zone)
	}
}

func TestCloudflareRequestRedactsTokenFromErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Invalid access token"}],"result":null}`))
	}))
	defer server.Close()
	previous := cloudflareAPIBaseURL
	cloudflareAPIBaseURL = server.URL
	defer func() { cloudflareAPIBaseURL = previous }()

	if _, err := cloudflareRequest[map[string]interface{}](context.Background(), http.MethodGet, "/verify", "super-secret-token", nil); err == nil || err.Error() != "Invalid access token" {
		t.Fatalf("cloudflareRequest() error = %v", err)
	}
}

func TestDeleteCloudflareDNSRecordUsesExactOwnedIdentifier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/zones/zone-1/dns_records/record-9" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"record-9"}}`))
	}))
	defer server.Close()
	previous := cloudflareAPIBaseURL
	cloudflareAPIBaseURL = server.URL
	defer func() { cloudflareAPIBaseURL = previous }()

	if err := deleteCloudflareDNSRecord(context.Background(), "token", "zone-1", "record-9"); err != nil {
		t.Fatalf("deleteCloudflareDNSRecord() error = %v", err)
	}
}

func TestCloudflareRecordAlreadyAbsentRecognizesOnlyNotFound(t *testing.T) {
	if !cloudflareRecordAlreadyAbsent(&cloudflareRequestError{StatusCode: http.StatusNotFound, Message: "not found"}) {
		t.Fatal("Cloudflare 404 was not recognized as an idempotent delete")
	}
	if cloudflareRecordAlreadyAbsent(&cloudflareRequestError{StatusCode: http.StatusForbidden, Message: "forbidden"}) {
		t.Fatal("Cloudflare authorization failure was treated as an idempotent delete")
	}
}
