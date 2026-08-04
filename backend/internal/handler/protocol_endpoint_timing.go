package handler

import "time"

type protocolEndpointMutationTiming struct {
	ValidationMS          int64 `json:"validation_ms"`
	TransactionMS         int64 `json:"transaction_ms"`
	TaskEnqueueMS         int64 `json:"task_enqueue_ms"`
	ResponsePreparationMS int64 `json:"response_preparation_ms"`
	ServerTotalMS         int64 `json:"server_total_ms"`
}

func protocolEndpointElapsedMilliseconds(start, end time.Time) int64 {
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func newProtocolEndpointMutationTiming(
	requestStartedAt time.Time,
	validationFinishedAt time.Time,
	transactionFinishedAt time.Time,
	taskEnqueueFinishedAt time.Time,
	responseFinishedAt time.Time,
) protocolEndpointMutationTiming {
	return protocolEndpointMutationTiming{
		ValidationMS:          protocolEndpointElapsedMilliseconds(requestStartedAt, validationFinishedAt),
		TransactionMS:         protocolEndpointElapsedMilliseconds(validationFinishedAt, transactionFinishedAt),
		TaskEnqueueMS:         protocolEndpointElapsedMilliseconds(transactionFinishedAt, taskEnqueueFinishedAt),
		ResponsePreparationMS: protocolEndpointElapsedMilliseconds(taskEnqueueFinishedAt, responseFinishedAt),
		ServerTotalMS:         protocolEndpointElapsedMilliseconds(requestStartedAt, responseFinishedAt),
	}
}
