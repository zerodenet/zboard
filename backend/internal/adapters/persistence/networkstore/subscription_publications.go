package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"sort"
)

// CurrentCredentialEndpointIDs resolves both membership sources as locking
// current reads. A locking SELECT outside a derived UNION does not make that
// UNION's nested reads current under MySQL repeatable-read isolation.
func CurrentCredentialEndpointIDs(tx *gorm.DB, groupID uint) ([]uint, error) {
	var direct, forward []uint
	if err := tx.Model(&model.NodeGroupEndpoint{}).Clauses(currentResourceReadLock(tx)).Where("node_group_id = ?", groupID).Order("protocol_endpoint_id").Pluck("protocol_endpoint_id", &direct).Error; err != nil {
		return nil, currentResourceReadError(err)
	}
	if err := tx.Table("node_group_network_entries AS membership").Clauses(currentResourceReadLock(tx)).Select("entry.endpoint_id").Joins("JOIN network_entries AS entry ON entry.id = membership.network_entry_id").Where("membership.node_group_id = ? AND entry.enabled = ?", groupID, true).Order("entry.endpoint_id").Pluck("entry.endpoint_id", &forward).Error; err != nil {
		return nil, currentResourceReadError(err)
	}
	seen := make(map[uint]bool, len(direct)+len(forward))
	ids := make([]uint, 0, len(direct)+len(forward))
	for _, id := range append(direct, forward...) {
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func EnqueueNodePublication(tx *gorm.DB, nodeID, endpointID, requestedBy uint) error {
	return enqueueNodePublicationForEndpoints(tx, nodeID, endpointID, requestedBy, []uint{endpointID})
}

func enqueueNodePublicationForEndpoints(tx *gorm.DB, nodeID, triggerEndpointID, requestedBy uint, knownEndpointIDs []uint) error {
	if nodeID == 0 {
		return nil
	}
	// Revocation callers already hold credential locks. Do not acquire endpoint
	// locks here: endpoint mutations lock the endpoint before its credentials.
	// The scan retains existing node-wide routing (including endpointID=0);
	// explicit current identities cover endpoints newer than the RR snapshot.
	var endpointIDs []uint
	if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", nodeID).Pluck("id", &endpointIDs).Error; err != nil {
		return err
	}
	endpointIDs = uniqueSortedIDs(append(endpointIDs, knownEndpointIDs...))
	var entryNodes []uint
	if len(endpointIDs) != 0 {
		if err := tx.Model(&model.NetworkEntry{}).Clauses(currentResourceReadLock(tx)).Where("node_id IS NOT NULL AND endpoint_id IN ?", endpointIDs).Order("node_id").Pluck("node_id", &entryNodes).Error; err != nil {
			return currentResourceReadError(err)
		}
	}
	if err := lockCurrentResourceNodes(tx, append([]uint{nodeID}, entryNodes...)); err != nil {
		return err
	}
	var previous uint
	for _, entryNode := range entryNodes {
		if entryNode == previous {
			continue
		}
		previous = entryNode
		if entryNode != nodeID {
			if err := EnqueuePublication(tx, entryNode, 0, requestedBy); err != nil {
				return err
			}
		}
	}
	return EnqueuePublication(tx, nodeID, triggerEndpointID, requestedBy)
}

// CurrentCredentialEndpoints returns current metadata under the same immediate
// lock discipline as routing; callers still own the enclosing business transaction.
func CurrentCredentialEndpoints(tx *gorm.DB, groupID uint) ([]model.ProtocolEndpoint, error) {
	ids, err := CurrentCredentialEndpointIDs(tx, groupID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	var endpoints []model.ProtocolEndpoint
	err = tx.Model(&model.ProtocolEndpoint{}).Clauses(currentResourceReadLock(tx)).Where("id IN ? AND is_active = ?", ids, true).Order("id").Find(&endpoints).Error
	if err != nil {
		return nil, currentResourceReadError(err)
	}
	nodeIDs := make([]uint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		nodeIDs = append(nodeIDs, endpoint.NodeID)
	}
	if err := lockCurrentResourceNodes(tx, nodeIDs); err != nil {
		return nil, err
	}
	sort.SliceStable(endpoints, func(i, j int) bool {
		if endpoints[i].SortOrder == endpoints[j].SortOrder {
			return endpoints[i].ID < endpoints[j].ID
		}
		return endpoints[i].SortOrder < endpoints[j].SortOrder
	})
	return endpoints, nil
}

// CredentialEndpointRoute is the minimal current endpoint identity needed for
// durable configuration routing, without loading endpoint configuration secrets.
type CredentialEndpointRoute struct {
	NodeID     uint
	EndpointID uint
}

// LockCurrentCredentialEndpointRoutes can prepare grant routing before any
// credential writes. Other callers may already retain credential locks: immediate
// acquisition returns a conflict instead of adding a reverse endpoint wait.
func LockCurrentCredentialEndpointRoutes(tx *gorm.DB, groupID uint) ([]CredentialEndpointRoute, error) {
	ids, err := CurrentCredentialEndpointIDs(tx, groupID)
	if err != nil || len(ids) == 0 {
		return nil, currentResourceReadError(err)
	}
	var nodes []CredentialEndpointRoute
	err = tx.Table("protocol_endpoints").Clauses(currentResourceReadLock(tx)).Select("node_id, id AS endpoint_id").Where("id IN ? AND is_active = ?", ids, true).Order("id").Scan(&nodes).Error
	if err != nil {
		return nil, currentResourceReadError(err)
	}
	nodeIDs := make([]uint, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.NodeID)
	}
	if err := lockCurrentResourceNodes(tx, nodeIDs); err != nil {
		return nil, err
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].NodeID == nodes[j].NodeID {
			return nodes[i].EndpointID < nodes[j].EndpointID
		}
		return nodes[i].NodeID < nodes[j].NodeID
	})
	return nodes, currentResourceReadError(err)
}

func EnqueueSubscriptionPublications(tx *gorm.DB, subscriptionID, requestedBy uint) error {
	var sub model.Subscription
	if err := tx.Clauses(currentResourceReadLock(tx)).Select("id", "node_group_id").First(&sub, subscriptionID).Error; err != nil {
		return currentResourceReadError(err)
	}
	nodes, err := LockCurrentCredentialEndpointRoutes(tx, sub.NodeGroupID)
	if err != nil {
		return err
	}
	for i := 0; i < len(nodes); {
		first := nodes[i]
		currentIDs := []uint{first.EndpointID}
		i++
		for i < len(nodes) && nodes[i].NodeID == first.NodeID {
			currentIDs = append(currentIDs, nodes[i].EndpointID)
			i++
		}
		if err := enqueueNodePublicationForEndpoints(tx, first.NodeID, first.EndpointID, requestedBy, currentIDs); err != nil {
			return err
		}
	}
	return nil
}
