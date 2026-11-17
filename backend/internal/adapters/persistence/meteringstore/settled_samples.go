package meteringstore

import (
	"sort"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// PendingFlowSamples is a separate read transaction, released before acquiring
// any mutable quota/topology authority. A matching runtime floor which dominates
// this sample proves that no new debit is possible. Unknown attribution, legacy
// identities and merged windows stay on the normal authoritative path.
func PendingFlowSamples(tx *gorm.DB, cipher metering.CredentialDecryptor, samples []metering.FlowSample) ([]metering.FlowSample, error) {
	keys := map[uint][]string{}
	for _, sample := range samples {
		if err := metering.ValidateFlowSample(sample); err != nil {
			return nil, err
		}
		if len(sample.Checkpoints) == 0 && sample.CoreInstanceID != "" && sample.Sequence > 0 {
			keys[sample.NodeID] = append(keys[sample.NodeID], metering.FlowUsageKey(sample.CoreInstanceID, sample.FlowID))
		}
	}
	nodes := make([]uint, 0, len(keys))
	for node := range keys {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	floors := map[uint]map[string]model.FlowUsage{}
	for _, node := range nodes {
		floors[node] = map[string]model.FlowUsage{}
		// Bound SQL lists even for callers outside the spool consumer. The
		// existing cold-page reader also shares its bounded transaction cache.
		for start := 0; start < len(keys[node]); start += 128 {
			rows, err := readFlowUsageCandidates(tx, node, keys[node][start:min(start+128, len(keys[node]))], "")
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				floors[node][row.FlowID] = row
			}
		}
	}
	pending := make([]metering.FlowSample, 0, len(samples))
	credentials := map[zeroFlowPrincipal]model.ProtocolCredential{}
	quotas := map[entitlementQuotaKey]bool{}
	for _, sample := range samples {
		floor, found := floors[sample.NodeID][metering.FlowUsageKey(sample.CoreInstanceID, sample.FlowID)]
		if len(sample.Checkpoints) > 0 || sample.CoreInstanceID == "" || sample.Sequence == 0 || !found || floor.Revision < sample.Sequence || floor.UploadBytes < sample.BytesUp || floor.DownloadBytes < sample.BytesDown {
			pending = append(pending, sample)
			continue
		}
		principal := zeroFlowPrincipal{sample.NodeID, sample.PrincipalKey}
		credential, known := credentials[principal]
		var err error
		if !known {
			credential, err = (credentialstore.PrincipalIndex{DB: tx, Cipher: cipher}).LookupPrepared(tx.Statement.Context, sample.NodeID, sample.PrincipalKey)
			if err == nil {
				credentials[principal] = credential
			}
		}
		if err == nil {
			err = metering.ValidateCredentialIngress(sample.InboundTag, credential.ProtocolEndpointID)
		}
		quotaKnown := false
		if err == nil {
			key := entitlementQuotaKey{credential.SubscriptionID, credential.EntitlementEpoch}
			var cached bool
			quotaKnown, cached = quotas[key]
			if !cached {
				// This is a non-locking proof, not an alternative quota writer. A new
				// report identity cannot hide lost historical authority merely because
				// its counters are already dominated. Missing/invalid evidence falls
				// through to the normal authoritative path and its original error.
				var present []int
				if e := tx.Raw(`SELECT 1 AS present FROM subscriptions WHERE id=? AND entitlement_epoch=? AND user_id=?
 UNION ALL SELECT 1 AS present FROM closed_entitlements WHERE subscription_id=? AND epoch=? AND user_id=? AND closing_flow_used>=0 AND flow_used>=closing_flow_used AND flow_used<=flow_total LIMIT 1`, credential.SubscriptionID, credential.EntitlementEpoch, credential.UserID, credential.SubscriptionID, credential.EntitlementEpoch, credential.UserID).Scan(&present).Error; e != nil {
					return nil, e
				}
				quotaKnown = len(present) > 0
				quotas[key] = quotaKnown
			}
		}
		if err != nil || !quotaKnown || credential.ID != floor.ProtocolCredentialID || (floor.ProtocolEndpointID != 0 && credential.ProtocolEndpointID != floor.ProtocolEndpointID) {
			pending = append(pending, sample)
		}
	}
	return pending, nil
}
