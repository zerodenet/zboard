package meteringstore

import (
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type zeroFlowPrincipal struct {
	nodeID    uint
	principal string
}

type zeroFlowUsageKey struct {
	nodeID uint
	flowID string
}

type zeroFlowReportKey struct {
	nodeID   uint
	reportID string
}

type zeroFlowCredentialRef struct {
	EntitlementEpoch   uint64
	ID                 uint
	SubscriptionID     uint
	ProtocolEndpointID uint
	PrincipalKey       string
	CredentialID       string
}

// Lives only inside one accounting transaction, bounded by the consumer batch.
// Locked subscription pointers include every preceding charge in that batch.
// No authentication or entitlement snapshot survives a commit or rollback.
type flowBatch struct {
	// SQLite owns the writer slot for this transaction. Cohorts are loaded only
	// after ordinary ownership/credential locks; rows remain live in sample order.
	usageCohorts       map[zeroFlowPrincipal][]metering.FlowSample
	usageLoaded        map[zeroFlowPrincipal]bool
	usages             map[zeroFlowUsageKey]model.FlowUsage
	closed             map[entitlementQuotaKey]*model.ClosedEntitlement
	dirtyClosed        map[entitlementQuotaKey]time.Time
	batchReports       bool
	credentials        map[zeroFlowPrincipal]zeroFlowCredentialRef
	subscriptions      map[uint]*model.Subscription
	endpoints          map[uint]model.ProtocolEndpoint
	touched            map[uint]time.Time
	dirtySubscriptions map[uint]time.Time
	records            []model.TrafficRecord
	usageRecords       []model.TrafficRecord
	recorded           map[zeroFlowReportKey]uint
}

func newFlowBatch(tx *gorm.DB) *flowBatch {
	return &flowBatch{
		closed:             make(map[entitlementQuotaKey]*model.ClosedEntitlement),
		dirtyClosed:        make(map[entitlementQuotaKey]time.Time),
		batchReports:       datastore.IsSQLite(tx),
		usageCohorts:       make(map[zeroFlowPrincipal][]metering.FlowSample),
		usageLoaded:        make(map[zeroFlowPrincipal]bool),
		usages:             make(map[zeroFlowUsageKey]model.FlowUsage),
		credentials:        make(map[zeroFlowPrincipal]zeroFlowCredentialRef),
		subscriptions:      make(map[uint]*model.Subscription),
		endpoints:          make(map[uint]model.ProtocolEndpoint),
		touched:            make(map[uint]time.Time),
		dirtySubscriptions: make(map[uint]time.Time),
		recorded:           make(map[zeroFlowReportKey]uint),
	}
}

func (s *flowBatch) loadReports(tx *gorm.DB, events []metering.FlowSample) error {
	if s.batchReports {
		for _, event := range events {
			key := zeroFlowPrincipal{event.NodeID, event.PrincipalKey}
			s.usageCohorts[key] = append(s.usageCohorts[key], event)
		}
	}
	if !s.batchReports {
		return nil
	}
	byNode := make(map[uint][]string)
	for _, event := range events {
		byNode[uint(event.NodeID)] = append(byNode[uint(event.NodeID)], strings.TrimSpace(event.EventID))
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

func (s *flowBatch) recordedEndpoint(tx *gorm.DB, key zeroFlowReportKey) (uint, bool, error) {
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

func (s *flowBatch) appendRecord(tx *gorm.DB, record model.TrafficRecord) error {
	if s.batchReports {
		s.records = append(s.records, record)
	} else {
		// Preserve database-equivalent duplicates later in this same batch.
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
	}
	s.usageRecords = append(s.usageRecords, record)
	s.recorded[zeroFlowReportKey{record.NodeID, record.ReportID}] = record.ProtocolEndpointID
	return nil
}

func (s *flowBatch) credential(cipher metering.CredentialDecryptor, tx *gorm.DB, nodeID uint, principal string) (zeroFlowCredentialRef, error) {
	key := zeroFlowPrincipal{nodeID, principal}
	if ref, ok := s.credentials[key]; ok {
		return ref, nil
	}
	credential, err := ResolveCompletionCredential(tx, cipher, nodeID, principal)
	if err != nil {
		return zeroFlowCredentialRef{}, err
	}
	ref := zeroFlowCredentialRef{credential.EntitlementEpoch, credential.ID, credential.SubscriptionID, credential.ProtocolEndpointID, credential.PrincipalKey, credential.CredentialID}
	s.credentials[key] = ref
	return ref, nil
}

func (s *flowBatch) subscription(tx *gorm.DB, id uint) (*model.Subscription, error) {
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

func (s *flowBatch) endpoint(tx *gorm.DB, id uint) (model.ProtocolEndpoint, error) {
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

func (s *flowBatch) flush(tx *gorm.DB) error {
	// Ledger rows and the final balance commit together. Chunk inserts to keep
	// SQL parameter counts bounded on both supported database engines.
	if len(s.records) > 0 {
		if err := tx.CreateInBatches(&s.records, 100).Error; err != nil {
			return err
		}
		// SQLite defers inserts to this boundary. Use the copies populated
		// with real ledger IDs when persisting daily projection intents.
		s.usageRecords = s.records
	}
	if err := RecordUsageProjections(tx, s.usageRecords); err != nil {
		return err
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
			"ended_at": sub.EndedAt, "end_reason": sub.EndReason,
		}).Error; err != nil {
			return err
		}
	}
	closedKeys := make([]entitlementQuotaKey, 0, len(s.dirtyClosed))
	for key := range s.dirtyClosed {
		closedKeys = append(closedKeys, key)
	}
	sort.Slice(closedKeys, func(i, j int) bool {
		if closedKeys[i].SubscriptionID != closedKeys[j].SubscriptionID {
			return closedKeys[i].SubscriptionID < closedKeys[j].SubscriptionID
		}
		return closedKeys[i].Epoch < closedKeys[j].Epoch
	})
	for _, key := range closedKeys {
		row := s.closed[key]
		if err := tx.Model(&model.ClosedEntitlement{}).Where("subscription_id = ? AND epoch = ?", key.SubscriptionID, key.Epoch).Updates(map[string]any{"flow_used": row.FlowUsed, "updated_at": s.dirtyClosed[key]}).Error; err != nil {
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

// loadUsage follows the existing attribution and quota locks. Never enable this
// map for MySQL: legacy keys and report identities follow database collation.
func (s *flowBatch) loadUsage(tx *gorm.DB, in metering.FlowSample, credential uint, current metering.FlowCounters) (model.FlowUsage, bool, bool, error) {
	if !s.batchReports {
		return LoadFlowUsage(tx, in.NodeID, in.CoreInstanceID, in.FlowID, credential, in.Revision, current)
	}
	cohort := zeroFlowPrincipal{in.NodeID, in.PrincipalKey}
	if !s.usageLoaded[cohort] {
		keys := make([]string, 0, 2*len(s.usageCohorts[cohort]))
		seen := make(map[string]bool)
		for _, sample := range s.usageCohorts[cohort] {
			for _, key := range []string{metering.FlowUsageKey(sample.CoreInstanceID, sample.FlowID), sample.FlowID} {
				if !seen[key] {
					seen[key] = true
					keys = append(keys, key)
				}
			}
		}
		sort.Strings(keys)
		for start := 0; start < len(keys); start += 200 {
			var rows []model.FlowUsage
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND flow_id IN ?", in.NodeID, keys[start:min(start+200, len(keys))]).Order("flow_id").Find(&rows).Error; err != nil {
				return model.FlowUsage{}, false, false, err
			}
			for _, row := range rows {
				s.usages[zeroFlowUsageKey{row.NodeID, row.FlowID}] = row
			}
		}
		s.usageLoaded[cohort] = true
	}
	key := metering.FlowUsageKey(in.CoreInstanceID, in.FlowID)
	candidates := make([]model.FlowUsage, 0, 2)
	if row, ok := s.usages[zeroFlowUsageKey{in.NodeID, key}]; ok {
		candidates = append(candidates, row)
	}
	if key != in.FlowID {
		if row, ok := s.usages[zeroFlowUsageKey{in.NodeID, in.FlowID}]; ok {
			candidates = append(candidates, row)
		}
	}
	return selectFlowUsage(candidates, key, in.FlowID, credential, in.Revision, current)
}

func (s *flowBatch) rememberUsage(previous string, row model.FlowUsage) {
	if !s.batchReports {
		return
	}
	if previous != "" && previous != row.FlowID {
		delete(s.usages, zeroFlowUsageKey{row.NodeID, previous})
	}
	s.usages[zeroFlowUsageKey{row.NodeID, row.FlowID}] = row
}
