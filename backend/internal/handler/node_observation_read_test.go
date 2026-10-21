package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestObservationHTTPReadsKeepCounterSource(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	assertObservationHTTPReads(t, h)
}
func TestMySQLObservationHTTPReadsKeepCounterSource(t *testing.T) {
	h, _ := newMySQLBusinessHandlers(t)
	assertObservationHTTPReads(t, h)
}
func assertObservationHTTPReads(t *testing.T, h *handlers) {
	t.Helper()
	actor := model.User{Email: "observation-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: actor.ID, Email: actor.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "observation-http-credential"
	encrypted, err := h.credentialCipher.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "observation-http", Config: "{}", IsEnabled: true, NodeCredential: encrypted}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	type item struct {
		ID          uint                        `json:"id"`
		ActiveFlows uint64                      `json:"active_flows"`
		BytesUp     uint64                      `json:"bytes_up"`
		BytesDown   uint64                      `json:"bytes_down"`
		Observation *networkcap.CounterEvidence `json:"observation"`
	}
	read := func(kind string, expected int) item {
		t.Helper()
		w := httptest.NewRecorder()
		url := fmt.Sprintf("/api/v1/nodes/%d", node.ID)
		if kind != "detail" {
			url = "/api/v1/nodes"
			if kind == "page" {
				url += fmt.Sprintf("?paged=true&node_id=%d", node.ID)
			}
		}
		r := announcementRequest(http.MethodGet, url, token, "")
		if kind == "detail" {
			h.NodeDetailHandler(w, r)
		} else {
			h.NodeListHandler(w, r)
		}
		if w.Code != expected {
			t.Fatalf("%s HTTP=%d expected=%d body=%s", kind, w.Code, expected, w.Body.String())
		}
		if expected != http.StatusOK {
			return item{}
		}
		if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), node.AssetTag) {
			t.Fatal("public source response leaked credentials or private asset tag")
		}
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		var out item
		if kind == "detail" {
			if err := json.Unmarshal(envelope.Data, &out); err != nil {
				t.Fatal(err)
			}
		} else {
			var items []item
			if kind == "page" {
				var page struct {
					Items []item `json:"items"`
				}
				if err := json.Unmarshal(envelope.Data, &page); err != nil {
					t.Fatal(err)
				}
				items = page.Items
			} else {
				if err := json.Unmarshal(envelope.Data, &items); err != nil {
					t.Fatal(err)
				}
			}
			if len(items) != 1 {
				t.Fatalf("%s missing node: %s", kind, w.Body.String())
			}
			out = items[0]
		}
		if out.ID != node.ID || out.Observation == nil {
			t.Fatalf("%s response lost source metadata: %s", kind, w.Body.String())
		}
		return out
	}
	occurred := time.Unix(1700000000, 123456789).UTC()
	received := occurred.Add(time.Second + 33*time.Nanosecond)
	event := zeroevent.Envelope{ID: "http-stats", NodeID: uint64(node.ID), AssetTag: node.AssetTag, Type: "stats.sampled", SourceID: "http-source", CoreInstanceID: "runtime", Sequence: 99, OccurredAt: occurred, ReceivedAt: received, Payload: []byte(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)}
	if err := h.projectZeroNodeEvents(context.Background(), []zeroevent.Envelope{event}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"detail", "page", "legacy"} {
		out := read(kind, http.StatusOK)
		if out.Observation.Known || out.Observation.StatsSourceKnown || out.Observation.CounterReceivedAt != nil {
			t.Fatal("pending intent pretended to be applied", kind, out)
		}
	}
	if _, err := h.services.NodeObservations.Drain(context.Background(), 32, 8*1024*1024); err != nil {
		t.Fatal(err)
	}
	var ref string
	for _, kind := range []string{"detail", "page", "legacy"} {
		out := read(kind, http.StatusOK)
		e := out.Observation
		if !e.Known || !e.LatestSourceKnown || !e.StatsSourceKnown || e.Source != "buffered_stats" || e.StatsReference == "" || e.CounterReceivedAt == nil || !e.CounterReceivedAt.Equal(received.Truncate(time.Millisecond)) || e.StatsOccurredAt == nil || !e.StatsOccurredAt.Equal(occurred.Truncate(time.Millisecond)) {
			t.Fatal("applied source lost its own position/time", kind, out)
		}
		if kind != "page" && (out.ActiveFlows != 3 || out.BytesUp != 100 || out.BytesDown != 200) {
			t.Fatal("HTTP counters differ from recorded source", kind, out)
		}
		if ref != "" && e.StatsReference != ref {
			t.Fatal("list/detail read different source", kind)
		}
		ref = e.StatsReference
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/%d/heartbeat", node.ID), strings.NewReader(fmt.Sprintf(`{"node_id":"%d","build_id":"observed-build","active_flows":9}`, node.ID)))
	r.Header.Set("Authorization", "Bearer "+secret)
	r.Header.Set("Content-Type", "application/json")
	h.NodeConnectorHeartbeatHandler(w, r)
	if w.Code != http.StatusOK {
		t.Fatal("heartbeat fixture failed", w.Code, w.Body.String())
	}
	out := read("detail", http.StatusOK)
	if out.ActiveFlows != 9 || out.BytesUp != 100 || out.BytesDown != 200 || out.Observation.Known || out.Observation.Source != "heartbeat" || out.Observation.StatsReference != ref {
		t.Fatal("heartbeat response borrowed or erased buffered evidence", out)
	}
	if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"detail", "page", "legacy"} {
		read(kind, http.StatusForbidden)
	}
}
