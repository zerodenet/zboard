package network

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestObservationMaterialPreservesIndependentPositionsAndLargeIdentity(t *testing.T) {
	latest := EventPosition{AssetTag: strings.Repeat("a", 64), ID: "flow100", CoreInstanceID: strings.Repeat("long-core-", 120000), Sequence: 100, OccurredAt: time.Unix(1700000000, 0).UTC(), ReceivedAt: time.Unix(1700000001, 0).UTC()}
	stats := latest
	stats.ID = "stats99"
	stats.Sequence = 99
	stats.AssetTag = strings.Repeat("b", 64)
	source := ObservationSource{Observation: NodeObservation{NodeID: 8, Latest: latest, StatsEvent: &stats, Stats: NodeStats{3, 4, 5}}, Kind: "retained", PrincipalKey: "raw principal", FlowID: "raw flow", Payload: []byte(`{"accounting_samples":[1,2],"first_sequence":7}`)}
	var b bytes.Buffer
	if err := WriteObservationSource(&b, source); err != nil {
		t.Fatal(err)
	}
	copy, err := ReadObservationSource(bytes.NewReader(b.Bytes()), uint64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copy, source) {
		t.Fatal("source/position/material changed")
	}
	if _, err := ReadObservationSource(bytes.NewReader(b.Bytes()), 12); err == nil {
		t.Fatal("untrusted length exceeded budget")
	}
}
func TestObservationMaterialPreservesLegacyUnknownAndZeroTimes(t *testing.T) {
	input := ObservationSource{Observation: NodeObservation{NodeID: 2, Latest: EventPosition{CoreInstanceID: "legacy"}}, LegacyFloor: true}
	var b bytes.Buffer
	if err := WriteObservationSource(&b, input); err != nil {
		t.Fatal(err)
	}
	output, err := ReadObservationSource(&b, uint64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, output) {
		t.Fatal("unknown source filled from current time/asset")
	}
}
