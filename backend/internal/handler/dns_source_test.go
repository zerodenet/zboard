package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestManagedDNSCommentPreservesOperatorTextAndUpdatesSource(t *testing.T) {
	record := network.ManagedDNSRecord{ID: 12, NodeID: 7}
	for _, tc := range []struct{ name, before, want string }{
		{"new", "", "[ZBoard dns=12 node=7]"},
		{"operator text", " 香港入口 | team: ops ", " 香港入口 | team: ops  | [ZBoard dns=12 node=7]"},
		{"repeat", "保留备注 | [ZBoard dns=12 node=7]", "保留备注 | [ZBoard dns=12 node=7]"},
		{"node changed", "保留备注 | [ZBoard dns=12 node=3]", "保留备注 | [ZBoard dns=12 node=7]"},
		{"source only", "[ZBoard dns=12 node=3]", "[ZBoard dns=12 node=7]"},
		{"ordinary mention", "Imported from ZBoard; node=3", "Imported from ZBoard; node=3 | [ZBoard dns=12 node=7]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := managedDNSComment(tc.before, record)
			if err != nil || got != tc.want {
				t.Fatalf("comment=%q err=%v want=%q", got, err, tc.want)
			}
			repeated, err := managedDNSComment(got, record)
			if err != nil || repeated != got {
				t.Fatalf("repeated comment=%q err=%v", repeated, err)
			}
		})
	}
	marker, _ := managedDNSComment("", record)
	limit := 500 - utf8.RuneCountInString(marker) - len(" | ")
	if comment, err := managedDNSComment(strings.Repeat("备", limit), record); err != nil || utf8.RuneCountInString(comment) != 500 {
		t.Fatalf("valid Unicode boundary rejected: %v", err)
	}
	if _, err := managedDNSComment(strings.Repeat("备", limit+1), record); err == nil {
		t.Fatal("oversized comment was silently truncated or accepted")
	}
}

func TestManagedDNSSourceIsSentOnCreateAndUpdate(t *testing.T) {
	for _, recordType := range []string{"A", "AAAA"} {
		for _, tc := range []struct {
			name, previous string
			update         bool
		}{
			{"create", "", false},
			{"backfill", "", true},
			{"operator comment", "人工说明", true},
			{"change node", "人工说明 | [ZBoard dns=12 node=3]", true},
		} {
			t.Run(recordType+"/"+tc.name, func(t *testing.T) {
				record := network.ManagedDNSRecord{ID: 12, NodeID: 7, RecordType: recordType, DomainName: "edge.example.test", RecordValue: "192.0.2.1", TTL: 120, Proxied: false}
				if recordType == "AAAA" {
					record.RecordValue = "2001:db8::1"
				}
				var existing *cloudflareRecord
				method, path := http.MethodPost, "/zones/zone-1/dns_records"
				if tc.update {
					existing = &cloudflareRecord{ID: "remote-9", Comment: tc.previous}
					method, path = http.MethodPatch, path+"/remote-9"
				}
				wantComment, _ := managedDNSComment(tc.previous, record)
				mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != method || r.URL.Path != path || r.Header.Get("Authorization") != "Bearer token" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					}
					var payload map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					data, _ := json.Marshal(payload)
					var sent cloudflareRecord
					_ = json.Unmarshal(data, &sent)
					if len(payload) != 6 || sent.Comment != wantComment || sent.Type != recordType || sent.Name != record.DomainName || sent.Content != record.RecordValue || sent.TTL != 120 || sent.Proxied {
						t.Errorf("unexpected payload: %s", data)
					}
					sent.ID = "remote-9"
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "result": sent})
				})
				applied, err := applyCloudflareManagedDNSRecord(t.Context(), "token", "zone-1", record, existing)
				if err != nil || applied.ID != "remote-9" || applied.Comment != wantComment {
					t.Fatalf("applied=%+v err=%v", applied, err)
				}
			})
		}
	}
}
