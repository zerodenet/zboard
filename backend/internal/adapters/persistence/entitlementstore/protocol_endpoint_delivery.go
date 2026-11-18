package entitlementstore

import (
	"encoding/json"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// A heartbeat or a deployment triggered by this endpoint does not establish
// that its listener was included. Use the frozen targets of the configuration
// actually acknowledged for the node.
func publishedProtocolEndpointRevisions(db *gorm.DB, endpoints []model.ProtocolEndpoint) (map[uint]map[uint]uint64, error) {
	seen := make(map[uint]bool)
	var nodeIDs []uint
	for _, endpoint := range endpoints {
		if endpoint.DeliveryRevision != 0 && !seen[endpoint.NodeID] {
			seen[endpoint.NodeID] = true
			nodeIDs = append(nodeIDs, endpoint.NodeID)
		}
	}
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	var receipts []struct {
		NodeID                                                uint
		ConfigSHA, DesiredSHA, AppliedSHA, KernelSHA, Targets string
	}
	if err := db.Table("node_credential_delivery_states AS receipt").
		Select("receipt.node_id, receipt.config_sha256 AS config_sha, deployment.desired_config_sha256 AS desired_sha, deployment.applied_config_sha256 AS applied_sha, kernel.applied_config_sha256 AS kernel_sha, deployment.protocol_endpoint_targets AS targets").
		Joins("JOIN protocol_deployments AS deployment ON deployment.id = receipt.deployment_id AND deployment.node_id = receipt.node_id").
		Joins("JOIN node_kernel_states AS kernel ON kernel.node_id = receipt.node_id").
		Where("receipt.node_id IN ? AND deployment.status = ? AND deployment.protocol_endpoint_targets <> ''", nodeIDs, "succeeded").
		Scan(&receipts).Error; err != nil {
		return nil, err
	}
	result := make(map[uint]map[uint]uint64, len(receipts))
	for _, receipt := range receipts {
		if len(receipt.ConfigSHA) != 64 || receipt.ConfigSHA != receipt.DesiredSHA || receipt.ConfigSHA != receipt.AppliedSHA || receipt.ConfigSHA != receipt.KernelSHA {
			continue
		}
		var revisions map[uint]uint64
		if err := json.Unmarshal([]byte(receipt.Targets), &revisions); err != nil {
			return nil, fmt.Errorf("decode protocol endpoint delivery for node %d: %w", receipt.NodeID, err)
		}
		result[receipt.NodeID] = revisions
	}
	return result, nil
}
