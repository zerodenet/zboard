package networkstore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func persistNewProtocolEndpointDelivery(tx *gorm.DB, admin model.User, endpoint model.ProtocolEndpoint, groupIDs []uint, now time.Time) (jobs.BatchSubmissionReceipt, error) {
	intent := network.BatchOperationContent{
		RequestedBy: admin.ID, Actor: admin.Email, NodeGroupIDs: groupIDs,
		EndpointIDsByNode: map[string][]uint{strconv.FormatUint(uint64(endpoint.NodeID), 10): {endpoint.ID}},
	}
	scope, err := json.Marshal(struct {
		NodeGroupIDs []uint `json:"node_group_ids"`
		NodeIDs      []uint `json:"node_ids"`
	}{groupIDs, []uint{endpoint.NodeID}})
	if err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	content, err := json.Marshal(intent)
	if err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	targets := make([]jobs.BatchSubmissionTarget, 0, len(groupIDs)+1)
	for _, groupID := range groupIDs {
		targets = append(targets, jobs.BatchSubmissionTarget{Type: "node_group", ID: groupID})
	}
	// Group items are prerequisites. The resource batch runner stops before
	// the one publish item if any of them fails or requeues.
	targets = append(targets, jobs.BatchSubmissionTarget{Type: "node", ID: endpoint.NodeID})
	return jobstore.PersistBatchSubmission(tx, jobs.BatchSubmissionActor{ID: admin.ID, Email: admin.Email}, jobs.BatchSubmission{
		Type: network.BatchNodeGroupSync, Scope: string(scope), Content: string(content),
		IdempotencyKey: fmt.Sprintf("node-protocol-delivery:%d", endpoint.ID),
		MaxAttempts:    3, ScheduledAt: now, Targets: targets,
	})
}
