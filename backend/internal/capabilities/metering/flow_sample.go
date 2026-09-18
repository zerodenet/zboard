package metering

import "errors"

// A sample reports cumulative counters; completion finalization remains a distinct operation.
type FlowSample = CompletedFlow
type FlowAccountingResult struct {
	NodeID, ProtocolEndpointID uint
	Exhausted                  bool
}

func ValidateFlowSample(in FlowSample) error {
	if in.NodeID == 0 || in.EventID == "" || in.FlowID == "" || in.PrincipalKey == "" || in.BytesUp < 0 || in.BytesDown < 0 {
		return errors.New("invalid cumulative flow sample")
	}
	return nil
}
