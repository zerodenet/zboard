package handler

import (
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type zeroFlowPrincipal struct {
	nodeID    uint
	principal string
}

type zeroFlowReportKey struct {
	nodeID   uint
	reportID string
}

type zeroFlowCredentialRef struct {
	ID                 uint
	SubscriptionID     uint
	ProtocolEndpointID uint
	PrincipalKey       string
	CredentialID       string
}

// Lives only inside one accounting transaction, bounded by the consumer batch.
// Locked subscription pointers include every preceding charge in that batch.
// No authentication or entitlement snapshot survives a commit or rollback.
type zeroFlowBatch struct {
	batchReports       bool
	credentials        map[zeroFlowPrincipal]zeroFlowCredentialRef
	subscriptions      map[uint]*model.Subscription
	endpoints          map[uint]model.ProtocolEndpoint
	touched            map[uint]time.Time
	dirtySubscriptions map[uint]time.Time
	records            []model.TrafficRecord
	recorded           map[zeroFlowReportKey]uint
}

func newZeroFlowBatch(tx *gorm.DB) *zeroFlowBatch {
	return &zeroFlowBatch{
		batchReports:       datastore.IsSQLite(tx),
		credentials:        make(map[zeroFlowPrincipal]zeroFlowCredentialRef),
		subscriptions:      make(map[uint]*model.Subscription),
		endpoints:          make(map[uint]model.ProtocolEndpoint),
		touched:            make(map[uint]time.Time),
		dirtySubscriptions: make(map[uint]time.Time),
		recorded:           make(map[zeroFlowReportKey]uint),
	}
}

func (s *zeroFlowBatch) loadReports(tx *gorm.DB, events []zeroevent.Envelope) error {
	if !s.batchReports {
		return nil
	}
	byNode := make(map[uint][]string)
	for _, event := range events {
		byNode[uint(event.NodeID)] = append(byNode[uint(event.NodeID)], strings.TrimSpace(event.ID))
	}
	nodes := make([]uint, 0, len(byNode))
	for id := range byNode {
		nodes = append(nodes, id)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	for _, nodeID := range nodes {
		ids := byNode[nodeID]
		for start := 0; start < len(ids); start += 200 {
			var records []model.TrafficRecord
			if err := tx.Select("node_id", "report_id", "protocol_endpoint_id").
				Where("node_id = ? AND report_id IN ?", nodeID, ids[start:min(start+200, len(ids))]).Find(&records).Error; err != nil {
				return err
			}
			for _, record := range records {
				s.recorded[zeroFlowReportKey{record.NodeID, record.ReportID}] = record.ProtocolEndpointID
			}
		}
	}
	return nil
}

func (s *zeroFlowBatch) recordedEndpoint(tx *gorm.DB, key zeroFlowReportKey) (uint, bool, error) {
	if endpoint, ok := s.recorded[key]; ok {
		return endpoint, true, nil
	}
	if s.batchReports {
		return 0, false, nil
	}
	// MySQL report IDs follow the column collation (including case/accent and
	// padding rules), not Go string equality. Let the database resolve identity.
	var record model.TrafficRecord
	read := tx.Select("protocol_endpoint_id").Where("node_id = ? AND report_id = ?", key.nodeID, key.reportID).Limit(1).Find(&record)
	return record.ProtocolEndpointID, read.RowsAffected != 0, read.Error
}

func (s *zeroFlowBatch) appendRecord(tx *gorm.DB, record model.TrafficRecord) error {
	if s.batchReports {
		s.records = append(s.records, record)
	} else {
		// Preserve database-equivalent duplicates later in this same batch.
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
	}
	s.recorded[zeroFlowReportKey{record.NodeID, record.ReportID}] = record.ProtocolEndpointID
	return nil
}

func (s *zeroFlowBatch) credential(h *handlers, tx *gorm.DB, nodeID uint, principal string) (zeroFlowCredentialRef, error) {
	key := zeroFlowPrincipal{nodeID, principal}
	if ref, ok := s.credentials[key]; ok {
		return ref, nil
	}
	credential, err := h.resolveZeroCompletionCredential(tx, nodeID, principal)
	if err != nil {
		return zeroFlowCredentialRef{}, err
	}
	ref := zeroFlowCredentialRef{credential.ID, credential.SubscriptionID, credential.ProtocolEndpointID, credential.PrincipalKey, credential.CredentialID}
	s.credentials[key] = ref
	return ref, nil
}

func (s *zeroFlowBatch) subscription(tx *gorm.DB, id uint) (*model.Subscription, error) {
	if sub, ok := s.subscriptions[id]; ok {
		return sub, nil
	}
	var sub model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, id).Error; err != nil {
		return nil, err
	}
	s.subscriptions[id] = &sub
	return &sub, nil
}

func (s *zeroFlowBatch) endpoint(tx *gorm.DB, id uint) (model.ProtocolEndpoint, error) {
	if endpoint, ok := s.endpoints[id]; ok {
		return endpoint, nil
	}
	var endpoint model.ProtocolEndpoint
	if err := tx.First(&endpoint, id).Error; err != nil {
		return endpoint, err
	}
	s.endpoints[id] = endpoint
	return endpoint, nil
}

func (s *zeroFlowBatch) flush(tx *gorm.DB) error {
	// Ledger rows and the final balance commit together. Chunk inserts to keep
	// SQL parameter counts bounded on both supported database engines.
	if len(s.records) > 0 {
		if err := tx.CreateInBatches(&s.records, 100).Error; err != nil {
			return err
		}
	}
	subscriptionIDs := make([]uint, 0, len(s.dirtySubscriptions))
	for id := range s.dirtySubscriptions {
		subscriptionIDs = append(subscriptionIDs, id)
	}
	sort.Slice(subscriptionIDs, func(i, j int) bool { return subscriptionIDs[i] < subscriptionIDs[j] })
	for _, id := range subscriptionIDs {
		sub := s.subscriptions[id]
		if err := tx.Model(sub).Updates(map[string]interface{}{
			"flow_used": sub.FlowUsed, "status": sub.Status, "updated_at": s.dirtySubscriptions[id],
		}).Error; err != nil {
			return err
		}
	}
	ids := make([]uint, 0, len(s.touched))
	for id := range s.touched {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := tx.Model(&model.ProtocolCredential{}).Where("id = ?", id).
			Updates(map[string]interface{}{"last_used_at": s.touched[id], "updated_at": s.touched[id]}).Error; err != nil {
			return err
		}
	}
	return nil
}
