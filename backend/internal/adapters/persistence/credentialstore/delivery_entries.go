package credentialstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Frozen deployment manifests remain immutable after an entry is deleted.
// Current projections include only extant identities. During acknowledgement
// or recovery, lock those identities so a concurrent deletion cannot race the FK.
func CurrentDeliveryEntries(tx *gorm.DB, targets []network.NetworkEntryDeliveryTarget, lock bool) ([]network.NetworkEntryDeliveryTarget, error) {
	ids := make([]uint, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, t.NetworkEntryID)
	}
	live := map[uint]bool{}
	for start := 0; start < len(ids); start += 256 {
		end := start + 256
		if end > len(ids) {
			end = len(ids)
		}
		var current []uint
		q := tx.Model(&model.NetworkEntry{}).Where("id IN ?", ids[start:end])
		if lock {
			q = q.Clauses(CurrentResourceReadLock(tx))
		}
		if err := q.Pluck("id", &current).Error; err != nil {
			return nil, CurrentResourceReadError(err)
		}
		for _, id := range current {
			live[id] = true
		}
	}
	out := make([]network.NetworkEntryDeliveryTarget, 0, len(targets))
	for _, t := range targets {
		if live[t.NetworkEntryID] {
			out = append(out, t)
		}
	}
	return out, nil
}
