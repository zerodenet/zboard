package metering

import (
	"errors"
	"time"
)

var ErrFlowWindowNeedsIndividualReports = errors.New("flow window requires individual report identity checks")

// Samples are panel-owned accounting inputs, never kernel configuration or wire fields.
// Checkpoints retain cumulative deltas for exact per-report multiplier rounding.
type FlowCheckpoint struct {
	Sequence           uint64
	BytesUp, BytesDown int64
	ReportID           string
}
type FlowSample struct {
	NodeID                                                             uint
	SourceID, CoreInstanceID, EventID, FlowID, PrincipalKey, EventType string
	Sequence, Revision                                                 uint64
	BytesUp, BytesDown                                                 int64
	OccurredAt                                                         time.Time
	Checkpoints                                                        []FlowCheckpoint
}
type FlowAccountingResult struct {
	NodeID, ProtocolEndpointID uint
	Exhausted                  bool
}

func ValidateFlowSample(in FlowSample) error {
	if in.NodeID == 0 || in.EventID == "" || in.FlowID == "" || in.PrincipalKey == "" || in.BytesUp < 0 || in.BytesDown < 0 {
		return errors.New("invalid cumulative flow sample")
	}
	if len(in.Checkpoints) == 0 {
		return nil
	}
	if in.CoreInstanceID == "" || len(in.Checkpoints) > 2000 {
		return errors.New("invalid cumulative flow checkpoint window")
	}
	var last FlowCheckpoint
	for i, point := range in.Checkpoints {
		if point.ReportID == "" || point.Sequence == 0 || point.BytesUp < 0 || point.BytesDown < 0 || (i > 0 && (point.Sequence <= last.Sequence || point.BytesUp < last.BytesUp || point.BytesDown < last.BytesDown)) {
			return errors.New("invalid cumulative flow checkpoint order")
		}
		last = point
	}
	if last.ReportID != in.EventID || last.Sequence != in.Sequence || last.BytesUp != in.BytesUp || last.BytesDown != in.BytesDown {
		return errors.New("flow checkpoint window does not end at sample")
	}
	return nil
}

// ChargeSample preserves the original rounded delta for every not-yet-settled
// checkpoint. A replay after a partial commit skips checkpoints using the durable
// runtime cursor, rather than trusting an in-memory deduplication cache.
func ChargeSample(in FlowSample, previous FlowCounters, sequence uint64, mode int16, multiplier, total, used int64) (ChargeDelta, error) {
	points := in.Checkpoints
	if len(points) == 0 {
		points = []FlowCheckpoint{{in.Sequence, in.BytesUp, in.BytesDown, in.EventID}}
	}
	result := ChargeDelta{}
	for _, point := range points {
		if len(in.Checkpoints) > 0 && (point.Sequence <= sequence || (sequence == 0 && point.BytesUp <= previous.Upload && point.BytesDown <= previous.Download)) {
			continue
		}
		raw, err := CheckedTrafficBytes(point.BytesUp, point.BytesDown, mode)
		if err != nil {
			return ChargeDelta{}, err
		}
		current := FlowCounters{Raw: raw, Upload: point.BytesUp, Download: point.BytesDown}
		charge, err := CalculateCharge(current, previous, multiplier, total, used)
		if err != nil {
			return ChargeDelta{}, err
		}
		if result.AccountingSamples == 0 {
			result.FirstSequence = point.Sequence
		}
		result.AccountingSamples++
		if multiplier > 0 && charge.Raw > 0 {
			// Preserve an auditable integer identity without retaining each
			// snapshot: billed = (raw*multiplier + rounding_numerator)/1000.
			remainder := (charge.Raw % 1000) * (multiplier % 1000) % 1000
			result.RoundingNumerator += uint64((1000 - remainder) % 1000)
		}
		result.Raw += charge.Raw
		result.Upload += charge.Upload
		result.Download += charge.Download
		result.Charged += charge.Charged
		used += charge.Charged
		previous = current
	}
	return result, nil
}
