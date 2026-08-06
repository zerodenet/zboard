package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestZeroAccountingEventsOnlySettleCompletedFlows(t *testing.T) {
	if !isZeroAccountingEvent("flow.completed") {
		t.Fatal("flow.completed must remain an accounting event")
	}
	for _, eventType := range []string{"flow.started", "flow.routed", "flow.updated", "flow.snapshot", "stats.sampled"} {
		if isZeroAccountingEvent(eventType) {
			t.Fatalf("%s unexpectedly became an accounting event", eventType)
		}
	}
}

func TestZeroCompletionAccountingBaselineContinuesLegacyActiveFlow(t *testing.T) {
	usage := model.FlowUsage{
		ProtocolCredentialID: 7,
		Status:               "active",
		Revision:             4,
		RawBytes:             300,
		UploadBytes:          100,
		DownloadBytes:        200,
	}
	flow := zeroFlowProjection{Revision: 5, BytesUp: 150, BytesDown: 250}

	baseline := zeroCompletionAccountingBaseline(usage, true, 7, flow, 400)
	if !baseline.ContinuesFlow || baseline.RawBytes != 300 || baseline.UploadBytes != 100 || baseline.DownloadBytes != 200 {
		t.Fatalf("unexpected active-flow baseline: %+v", baseline)
	}
}

func TestZeroCompletionAccountingBaselineResetsReusedOrCompletedFlow(t *testing.T) {
	tests := []struct {
		name         string
		usage        model.FlowUsage
		credentialID uint
		flow         zeroFlowProjection
		cumulative   int64
	}{
		{
			name: "completed cursor",
			usage: model.FlowUsage{
				ProtocolCredentialID: 7, Status: "completed", Revision: 4,
				RawBytes: 300, UploadBytes: 100, DownloadBytes: 200,
			},
			credentialID: 7,
			flow:         zeroFlowProjection{Revision: 5, BytesUp: 150, BytesDown: 250},
			cumulative:   400,
		},
		{
			name: "counter reset",
			usage: model.FlowUsage{
				ProtocolCredentialID: 7, Status: "active", Revision: 9,
				RawBytes: 300, UploadBytes: 100, DownloadBytes: 200,
			},
			credentialID: 7,
			flow:         zeroFlowProjection{Revision: 1, BytesUp: 10, BytesDown: 20},
			cumulative:   30,
		},
		{
			name: "revision rollback",
			usage: model.FlowUsage{
				ProtocolCredentialID: 7, Status: "active", Revision: 9,
				RawBytes: 30, UploadBytes: 10, DownloadBytes: 20,
			},
			credentialID: 7,
			flow:         zeroFlowProjection{Revision: 1, BytesUp: 100, BytesDown: 200},
			cumulative:   300,
		},
		{
			name: "different credential",
			usage: model.FlowUsage{
				ProtocolCredentialID: 8, Status: "active", Revision: 4,
				RawBytes: 30, UploadBytes: 10, DownloadBytes: 20,
			},
			credentialID: 7,
			flow:         zeroFlowProjection{Revision: 5, BytesUp: 100, BytesDown: 200},
			cumulative:   300,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseline := zeroCompletionAccountingBaseline(test.usage, true, test.credentialID, test.flow, test.cumulative)
			if baseline != (zeroCompletionBaseline{}) {
				t.Fatalf("stale flow cursor was reused: %+v", baseline)
			}
		})
	}
}
