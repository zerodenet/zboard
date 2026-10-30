package application

import (
	"sort"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func managedFlowInputs(tx *gorm.DB, flows []metering.FlowSample) ([]metering.FlowSample, int, error) {
	if len(flows) == 0 {
		return flows, 0, nil
	}
	seen := map[uint]bool{}
	ids := make([]uint, 0)
	for _, flow := range flows {
		if !seen[flow.NodeID] {
			ids = append(ids, flow.NodeID)
			seen[flow.NodeID] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var nodes []model.Node
	if err := tx.Clauses(credentialstore.CurrentResourceReadLock(tx)).Select("id,lifecycle_status").Where("id IN ?", ids).Order("id").Find(&nodes).Error; err != nil {
		return nil, 0, credentialstore.CurrentResourceReadError(err)
	}
	closed := map[uint]bool{}
	for _, node := range nodes {
		closed[node.ID] = network.NodeManagementClosed(node.LifecycleStatus)
	}
	accepted := make([]metering.FlowSample, 0, len(flows))
	held := 0
	for _, flow := range flows {
		if closed[flow.NodeID] {
			held++
		} else {
			accepted = append(accepted, flow)
		}
	}
	return accepted, held, nil
}
