package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestBufferedHTTPAssetStampComesOnlyFromAuthenticatedNode(t *testing.T) {
	h, _ := accountingBenchmarkFixture(t)
	node, runtime := receiptFixture(t, h)
	send := func(id string) {
		t.Helper()
		body, err := json.Marshal(map[string]any{"schema_id": "zero.event.v1", "event_id": id, "event_type": "stats.sampled", "source_id": fmt.Sprintf("node-%d", node.ID), "core_instance_id": strings.Repeat("long-runtime-", 30), "sequence": 1, "asset_tag": "wire-forged", "payload": map[string]any{"active_sessions": 1, "bytes_up": 0, "bytes_down": 0, "asset_tag": "payload-forged"}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/zero/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer receipt-test-secret")
		response := httptest.NewRecorder()
		h.ZeroEventHandler(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("authenticated append HTTP%d: %s", response.Code, response.Body.String())
		}
	}
	send("authenticated-tag")
	// Pin this test's captured cache entry expiration, not production TTL. A
	// controlled DB label change must not rewrite already captured admissions.
	value, ok := h.zeroEventAuthCache.Load(node.ID)
	if !ok {
		t.Fatal("auth cache absent")
	}
	cached := value.(zeroEventCredentialCacheEntry)
	cached.expiresAt = time.Now().Add(time.Hour)
	h.zeroEventAuthCache.Store(node.ID, cached)
	if err := h.db.Table("nodes").Where("id = ?", node.ID).Update("asset_tag", strings.Repeat("b", 64)).Error; err != nil {
		t.Fatal(err)
	}
	send("cached-tag")
	batch, err := runtime.spool.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 2 {
		t.Fatal("durable admission samples absent", err)
	}
	for _, event := range batch.Events {
		if event.AssetTag != node.AssetTag || event.AssetTag == "" || len(event.CoreInstanceID) <= 128 {
			t.Fatal("wire/current row replaced captured asset or narrowed CoreID")
		}
	}
}

func TestObservationPositionsKeepTheirOwnAssetAndUnknownMaterial(t *testing.T) {
	for _, statsTag := range []string{"asset-b", ""} {
		events := []zeroevent.Envelope{{ID: "stats", NodeID: 7, Type: "stats.sampled", AssetTag: statsTag, CoreInstanceID: "core", Sequence: 1, Payload: json.RawMessage(`{"active_sessions":1,"bytes_up":3,"bytes_down":0}`)}, {ID: "flow", NodeID: 7, Type: "flow.updated", AssetTag: "asset-a", CoreInstanceID: "core", Sequence: 2}}
		observation := zeroNodeObservation(aggregateZeroNodeEvents(events)[7])
		if observation.Latest.AssetTag != "asset-a" || observation.StatsEvent == nil || observation.StatsEvent.AssetTag != statsTag {
			t.Fatal("stats borrowed latest asset")
		}
		if !observation.Latest.ReceivedAt.IsZero() || !observation.StatsEvent.OccurredAt.IsZero() {
			t.Fatal("mapping manufactured replay-time evidence")
		}
		if again := zeroNodeObservation(aggregateZeroNodeEvents(events)[7]); !reflect.DeepEqual(observation, again) {
			t.Fatal("replay changed captured source")
		}
	}
}

func TestMixedAssetFlowSamplesPreserveOrderRoundingAndReplay(t *testing.T) {
	for _, middle := range []string{"asset-b", ""} {
		t.Run(middle, func(t *testing.T) {
			h, credential := accountingBenchmarkFixture(t)
			if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", credential.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
				t.Fatal(err)
			}
			events := repeatedFlowEvents(credential, 3)
			events[0].AssetTag = "asset-a"
			events[1].AssetTag = middle
			events[2].AssetTag = "asset-a"
			samples, err := zeroFlowSamples(events)
			if err != nil || len(samples) != 3 {
				t.Fatal("A/B/A merged across a label transition", err)
			}
			for i, sample := range samples {
				if sample.EventID != events[i].ID || sample.Sequence != events[i].Sequence {
					t.Fatal("optimization reordered financial report identities")
				}
			}
			for i := 0; i < 2; i++ {
				if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
					t.Fatal(err)
				}
			}
			assertAccountingTotal(t, h, credential.SubscriptionID, 15, subStatusActive)
			var records []model.TrafficRecord
			if err := h.db.Find(&records).Error; err != nil {
				t.Fatal(err)
			}
			var total, first uint64
			var rounding int64
			for _, record := range records {
				var evidence struct {
					Samples  uint64 `json:"accounting_samples"`
					First    uint64 `json:"first_sequence"`
					Rounding int64  `json:"rounding_numerator"`
				}
				if err := json.Unmarshal([]byte(record.Meta), &evidence); err != nil {
					t.Fatal(err)
				}
				total += evidence.Samples
				rounding += evidence.Rounding
				if first == 0 || evidence.First < first {
					first = evidence.First
				}
			}
			if total != 3 || first != 1 || rounding != 1500 {
				t.Fatal("sample/firstsequence/rounding evidence changed across assets")
			}
			duplicate := append([]zeroevent.Envelope(nil), events[:2]...)
			duplicate[1].ID = duplicate[0].ID
			if got, err := zeroFlowSamples(duplicate); err != nil || len(got) != 2 {
				t.Fatal("tag changed duplicate-report guard", err)
			}
		})
	}
}

func TestMixedAssetOtherFlowCannotReorderEarlierSample(t *testing.T) {
	events := repeatedFlowEvents(model.ProtocolCredential{NodeID: 7, PrincipalKey: "principal"}, 3)
	events[0].AssetTag = "asset-a"
	events[1].AssetTag = "asset-b"
	events[2].AssetTag = "asset-a"
	events[1].FlowID = "other-flow"
	events[1].Payload = json.RawMessage(`{"flow_id":"other-flow","traffic":{"bytes_up":2,"bytes_down":4}}`)
	got, err := zeroFlowSamples(events)
	if err != nil || len(got) != 3 {
		t.Fatal("mixed other-flow erased a financial boundary", err)
	}
	for i := range got {
		if got[i].EventID != events[i].ID {
			t.Fatal("different-flow asset transition reordered original reports")
		}
	}
	// Identical-label windows retain the existing per-flow optimization.
	events[1].AssetTag = "asset-a"
	got, err = zeroFlowSamples(events)
	if err != nil || len(got) != 2 {
		t.Fatal("single-label window changed original flow reduction", err)
	}
}
