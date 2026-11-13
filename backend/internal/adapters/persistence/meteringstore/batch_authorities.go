package meteringstore

import (
	"errors"
	"sort"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNativeAuthorityChanged = errors.New("native accounting authority changed during discovery")

// Discovery never grants authority. Native MySQL callers partition by quota
// before opening this transaction. Own that quota before identity/ledger writes,
// then revalidate shared credential/topology fences. SQLite keeps its serialized
// writer/cohort path; profile rows and replaceable activity hints are not locked.
func (s *flowBatch) lockAuthorities(tx *gorm.DB, cipher metering.CredentialDecryptor, events []metering.FlowSample) error {
	if datastore.IsSQLite(tx) {
		return nil
	}
	candidates := map[zeroFlowPrincipal]model.ProtocolCredential{}
	subs := map[uint]bool{}
	index := credentialstore.PrincipalIndex{DB: tx, Cipher: cipher}
	for _, e := range events {
		k := zeroFlowPrincipal{e.NodeID, e.PrincipalKey}
		if _, ok := candidates[k]; ok {
			continue
		}
		c, err := index.LookupPrepared(tx.Statement.Context, e.NodeID, e.PrincipalKey)
		// Defer attribution errors to original sample order: a database-equivalent
		// report earlier in this batch may make this sample an ordinary duplicate.
		if err != nil {
			if tx.Statement.Context.Err() != nil {
				return tx.Statement.Context.Err()
			}
			if errors.Is(err, credentialstore.ErrPrincipalIndexPending) && !errors.Is(err, credentialstore.ErrPrincipalIndexIncomplete) {
				err = metering.ErrPrincipalAmbiguous
			}
			if s.discoveryErrors == nil {
				s.discoveryErrors = map[zeroFlowPrincipal]error{}
			}
			s.discoveryErrors[k] = err
			continue
		}
		if s.expectedQuota != nil && (c.SubscriptionID != s.expectedQuota.SubscriptionID || c.EntitlementEpoch != s.expectedQuota.Epoch) {
			return ErrNativeAuthorityChanged
		}
		candidates[k] = c
		subs[c.SubscriptionID] = true
	}
	// Do not let a future caller accidentally restore a cross-quota transaction.
	var scope *QuotaIdentity
	for _, c := range candidates {
		q := QuotaIdentity{c.SubscriptionID, c.EntitlementEpoch}
		if scope != nil && *scope != q {
			return errors.New("native accounting batch must be partitioned by subscription and epoch")
		}
		copy := q
		scope = &copy
	}
	for _, id := range sortedAuthorityIDs(subs) {
		var sub model.Subscription
		read := tx.Where("id=?", id).Limit(1).Find(&sub)
		if read.Error != nil {
			return read.Error
		}
		if read.RowsAffected == 0 {
			archived, e := deletedSubscriptionAttribution(tx, id)
			if errors.Is(e, gorm.ErrRecordNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			sub = archived
		}
		current := false
		for _, c := range candidates {
			if c.SubscriptionID == id && c.EntitlementEpoch == sub.EntitlementEpoch && sub.Status != "deleted" {
				current = true
			}
		}
		if current {
			// Only this quota's current epoch needs its mutable subscription row.
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, id).Error; e != nil {
				return e
			}
		}
		s.subscriptions[id] = &sub
	}
	// Historical budgets are independent of the current subscription row.
	for _, c := range candidates {
		sub := s.subscriptions[c.SubscriptionID]
		if sub != nil && (sub.Status == "deleted" || c.EntitlementEpoch != sub.EntitlementEpoch) {
			key := entitlementQuotaKey{sub.ID, c.EntitlementEpoch}
			if s.closed[key] == nil {
				row, e := loadClosedEntitlement(tx, *sub, c.EntitlementEpoch)
				if e != nil {
					return e
				}
				s.closed[key] = &row
			}
		}
	}
	// Only now take identity/topology fences. Waiting on a subscription must
	// not hold a node lock that blocks heartbeat, deletion or observation.
	nodes := map[uint]bool{}
	for _, e := range events {
		nodes[e.NodeID] = true
	}
	for _, id := range sortedAuthorityIDs(nodes) {
		var n model.Node
		if err := tx.Clauses(credentialstore.CurrentResourceReadLock(tx)).Select("id").Where("id=?", id).Find(&n).Error; err != nil {
			return credentialstore.CurrentResourceReadError(err)
		}
	}
	keys := make([]zeroFlowPrincipal, 0, len(candidates))
	for k, c := range candidates {
		// A committed report receipt remains valid after owner/subscription
		// deletion. Missing current parents only reject a NEW contribution,
		// once the fenced report lookup below proves it is not a replay.
		if s.subscriptions[c.SubscriptionID] == nil {
			if s.discoveryErrors == nil {
				s.discoveryErrors = map[zeroFlowPrincipal]error{}
			}
			s.discoveryErrors[k] = gorm.ErrRecordNotFound
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := candidates[keys[i]], candidates[keys[j]]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if keys[i].nodeID != keys[j].nodeID {
			return keys[i].nodeID < keys[j].nodeID
		}
		return keys[i].principal < keys[j].principal
	})
	// Topology ownership uses NOWAIT, matching commerce; never reverse-wait
	// behind an endpoint edit while retaining accounting subscription locks.
	for _, k := range keys {
		c := candidates[k]
		if err := credentialstore.LockCurrentEndpointParents(tx, c.NodeID, c.ProtocolEndpointID); err != nil {
			return err
		}
	}
	for _, k := range keys {
		c := candidates[k]
		var actual model.ProtocolCredential
		if err := tx.Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"}).First(&actual, c.ID).Error; err != nil {
			return credentialstore.CurrentResourceReadError(err)
		}
		if actual.EntitlementEpoch != c.EntitlementEpoch || actual.NodeID != c.NodeID || actual.UserID != c.UserID || actual.SubscriptionID != c.SubscriptionID || actual.ProtocolEndpointID != c.ProtocolEndpointID || s.subscriptions[c.SubscriptionID].UserID != c.UserID || !completionPrincipalMatches(cipher, k.nodeID, k.principal, actual) {
			return ErrNativeAuthorityChanged
		}
		s.credentials[k] = zeroFlowCredentialRef{actual.EntitlementEpoch, actual.ID, actual.SubscriptionID, actual.ProtocolEndpointID, actual.PrincipalKey, actual.CredentialID}
	}
	return nil
}
func sortedAuthorityIDs(set map[uint]bool) []uint {
	ids := make([]uint, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
