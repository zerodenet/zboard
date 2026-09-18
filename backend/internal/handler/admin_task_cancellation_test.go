package handler

import (
	"context"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCanceledBatchDoesNotStartPendingItems(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// No database is needed: cancellation must be checked before claiming or
	// performing any item, including the leading group-reconciliation operation.
	h := &handlers{}
	for _, kind := range []string{taskTypeNodeDetect, taskTypeNodeGroupSync, taskTypeEmail} {
		result := h.executeTaskItemsContext(ctx, model.Task{Type: kind}, []model.TaskItem{{ID: 1, TargetType: "node_group"}}, "lease")
		if len(result) != 1 || result[0] != context.Canceled.Error() {
			t.Fatal(result)
		}
	}
}
