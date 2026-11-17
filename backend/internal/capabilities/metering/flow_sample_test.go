package metering

import (
	"math"
	"testing"
)

func TestChargeSamplePreservesPerReportRoundingAndPartialReplay(t *testing.T) {
	sample := FlowSample{NodeID: 1, EventID: "end", CoreInstanceID: "core", FlowID: "f", PrincipalKey: "p", Sequence: 3, BytesUp: 3, BytesDown: 6, Checkpoints: []FlowCheckpoint{{1, 1, 2, "first"}, {2, 2, 4, "second"}, {3, 3, 6, "end"}}}
	if err := ValidateFlowSample(sample); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []int16{0, 1, 2} {
		for _, multiplier := range []int64{1, 500, 1000, 1501, 2000} {
			for _, quota := range []int64{1, 1000} {
				previous := FlowCounters{}
				used := int64(0)
				for _, point := range sample.Checkpoints {
					raw, _ := CheckedTrafficBytes(point.BytesUp, point.BytesDown, mode)
					current := FlowCounters{raw, point.BytesUp, point.BytesDown}
					delta, err := CalculateCharge(current, previous, multiplier, quota, used)
					if err != nil {
						t.Fatal(err)
					}
					used += delta.Charged
					previous = current
				}
				got, err := ChargeSample(sample, FlowCounters{}, 0, mode, multiplier, quota, 0)
				if err != nil || got.Charged != used || got.FlowCounters != previous {
					t.Fatalf("mode=%d multiplier=%d quota=%d got=%+v want=%+v charged=%d err=%v", mode, multiplier, quota, got, previous, used, err)
				}
				firstRaw, _ := CheckedTrafficBytes(1, 2, mode)
				first, _ := CalculateCharge(FlowCounters{firstRaw, 1, 2}, FlowCounters{}, multiplier, quota, 0)
				replay, err := ChargeSample(sample, FlowCounters{firstRaw, 1, 2}, 1, mode, multiplier, quota, first.Charged)
				if err != nil || replay.Charged+first.Charged != used {
					t.Fatalf("partial replay=%+v first=%+v want=%d err=%v", replay, first, used, err)
				}
			}
		}
	}
}

func TestFlowCheckpointValidationRejectsRegressionAndAmbiguousWindows(t *testing.T) {
	base := FlowSample{NodeID: 1, EventID: "end", CoreInstanceID: "core", FlowID: "f", PrincipalKey: "p", Sequence: 2, BytesUp: 2, BytesDown: 4, Checkpoints: []FlowCheckpoint{{1, 1, 2, "first"}, {2, 2, 4, "end"}}}
	for _, change := range []func(*FlowSample){
		func(s *FlowSample) { s.Checkpoints[1].Sequence = 1 },
		func(s *FlowSample) { s.Checkpoints[0].BytesUp = 3 },
		func(s *FlowSample) { s.BytesUp = 3 },
		func(s *FlowSample) { s.CoreInstanceID = "" },
	} {
		sample := base
		sample.Checkpoints = append([]FlowCheckpoint(nil), base.Checkpoints...)
		change(&sample)
		if ValidateFlowSample(sample) == nil {
			t.Fatal("accepted unsafe checkpoint window")
		}
	}
	sample := base
	sample.BytesUp = math.MaxInt64
	sample.Checkpoints = []FlowCheckpoint{{1, 1, 2, "first"}, {2, math.MaxInt64, 4, "end"}}
	if _, err := ChargeSample(sample, FlowCounters{}, 0, 0, 1000, math.MaxInt64, 0); err == nil {
		t.Fatal("accepted overflowing counters")
	}
}
