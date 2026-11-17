package meteringstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
)

type QuotaIdentity struct {
	SubscriptionID uint
	Epoch          uint64
}
type FlowPartition struct {
	Quota         *QuotaIdentity
	Flows         []metering.FlowSample
	CredentialIDs []uint
}

// Contiguous partitions preserve original order, including MySQL-equivalent
// report IDs attributed to different principals. No regrouping may make a later
// duplicate win. Each transaction owns one quota and at most 64 samples.
func PlanFlowPartitions(ctx context.Context, db *gorm.DB, cipher metering.CredentialDecryptor, events []metering.FlowSample) ([]FlowPartition, error) {
	index := credentialstore.PrincipalIndex{DB: db, Cipher: cipher}
	type discovery struct {
		quota QuotaIdentity
		id    uint
		found bool
	}
	cache := map[zeroFlowPrincipal]discovery{}
	parts := []FlowPartition{}
	for _, event := range events {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		k := zeroFlowPrincipal{event.NodeID, event.PrincipalKey}
		d, ok := cache[k]
		if !ok {
			c, e := index.LookupPrepared(ctx, event.NodeID, event.PrincipalKey)
			// Defer attribution errors until the fenced receipt lookup; an already
			// committed report remains acknowledgeable without a current principal.
			if e == nil {
				d = discovery{QuotaIdentity{c.SubscriptionID, c.EntitlementEpoch}, c.ID, true}
			}
			cache[k] = d
		}
		appendToLast := len(parts) > 0 && d.found && parts[len(parts)-1].Quota != nil && *parts[len(parts)-1].Quota == d.quota && len(parts[len(parts)-1].Flows) < 64
		if !appendToLast {
			p := FlowPartition{}
			if d.found {
				q := d.quota
				p.Quota = &q
			}
			parts = append(parts, p)
		}
		p := &parts[len(parts)-1]
		p.Flows = append(p.Flows, event)
		if d.found {
			p.CredentialIDs = append(p.CredentialIDs, d.id)
		}
	}
	return parts, nil
}
