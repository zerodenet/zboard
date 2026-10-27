package commercestore

import (
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	orderEvidencePageSize        = 200
	orderEvidenceByteLimit int64 = 32 << 20
	orderEvidenceRowLimit  int64 = 1 << 20
	orderIdentityByteLimit       = 512 << 10
	// Closing arbitrary pending/held attempts is different from reading paid
	// provenance. Preserve its existing mutable-work bound independently.
	mutableSubscriptionOrderLimit = 1000
)

type orderEvidenceIdentity struct {
	ID            uint
	Status        string
	EvidenceBytes int64
}

type subscriptionOrderScope struct {
	Owner, Subscription uint
	StartAt             time.Time
	IDs                 []uint
}

func evidenceLengthSQL(db *gorm.DB) string {
	columns := []string{"purchase_snapshot", "entitlement_value_snapshot", "change_snapshot"}
	result := "0"
	for _, column := range columns {
		if db.Dialector.Name() == "sqlite" {
			result += "+length(CAST(COALESCE(" + column + ",'') AS BLOB))"
		} else {
			result += "+OCTET_LENGTH(COALESCE(" + column + ",''))"
		}
	}
	return result + " AS evidence_bytes"
}

// Page identity and byte lengths before loading text. A giant historical JSON
// value cannot force an unbounded allocation before the budget is checked.
func visitOrderEvidence(query *gorm.DB, visit func([]orderEvidenceIdentity) error) error {
	return visitOrderMetadata(query, true, visit)
}

func visitOrderMetadata(query *gorm.DB, checkBody bool, visit func([]orderEvidenceIdentity) error) error {
	var after uint
	var bytes int64
	identities, mutable := 0, 0
	for {
		var page []orderEvidenceIdentity
		if err := query.Session(&gorm.Session{}).Select("id,status,"+evidenceLengthSQL(query)).Where("id > ?", after).Order("id ASC").Limit(orderEvidencePageSize).Find(&page).Error; err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		for _, row := range page {
			if row.ID <= after || row.EvidenceBytes < 0 || checkBody && (row.EvidenceBytes > orderEvidenceRowLimit || row.EvidenceBytes > orderEvidenceByteLimit-bytes) {
				return commerce.ErrSubscriptionEvidenceBudget
			}
			bytes += row.EvidenceBytes
			identities++
			if identities > orderIdentityByteLimit/8 {
				return commerce.ErrSubscriptionEvidenceBudget
			}
			if row.Status != "paid" {
				mutable++
				if mutable > mutableSubscriptionOrderLimit {
					return commerce.ErrSubscriptionEvidenceBudget
				}
			}
			after = row.ID
		}
		if err := visit(page); err != nil {
			return err
		}
		if len(page) < orderEvidencePageSize {
			return nil
		}
	}
}

func readOrderEvidence(query *gorm.DB, columns ...string) ([]model.Order, error) {
	var out []model.Order
	err := visitOrderEvidence(query, func(page []orderEvidenceIdentity) error {
		ids := make([]uint, len(page))
		for i, row := range page {
			ids[i] = row.ID
		}
		var rows []model.Order
		q := query.Session(&gorm.Session{}).Where("id IN ?", ids).Order("id ASC").Limit(orderEvidencePageSize)
		if len(columns) > 0 {
			q = q.Select(columns)
		} else {
			q = q.Omit("raw_callback")
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(page) {
			return commerce.ErrSubscriptionQuoteChanged
		}
		for i, row := range rows {
			if row.ID != ids[i] {
				return commerce.ErrSubscriptionQuoteChanged
			}
		}
		out = append(out, rows...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func readOrderScopeIDs(query *gorm.DB) ([]uint, error) {
	var ids []uint
	err := visitOrderMetadata(query, false, func(page []orderEvidenceIdentity) error {
		for _, row := range page {
			ids = append(ids, row.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// All consistent discovery occurs before the authority transaction. Inside it,
// exact primary keys are locked in ascending bounded chunks before the account.
// No secondary-index history range is held while waiting for that account.
func prepareSubscriptionOrderScope(db *gorm.DB, owner, subscription uint) (subscriptionOrderScope, error) {
	out := subscriptionOrderScope{Owner: owner, Subscription: subscription}
	var sub model.Subscription
	err := db.Select("id,start_at").Where("id = ? AND user_id = ?", subscription, owner).First(&sub).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return out, err
	}
	out.StartAt = sub.StartAt
	out.IDs, err = readOrderScopeIDs(terminationOrders(db, owner, subscription, out.StartAt))
	return out, err
}

func lockSubscriptionOrderIDs(tx *gorm.DB, scope subscriptionOrderScope) error {
	for start := 0; start < len(scope.IDs); start += orderEvidencePageSize {
		ids := scope.IDs[start:min(start+orderEvidencePageSize, len(scope.IDs))]
		var rows []model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id IN ? AND user_id = ?", ids, scope.Owner).Order("id ASC").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(ids) {
			return commerce.ErrSubscriptionQuoteChanged
		}
		for i, row := range rows {
			if row.ID != ids[i] {
				return commerce.ErrSubscriptionQuoteChanged
			}
		}
	}
	return nil
}

// Call only after the account's current lock, before new financial mutations.
// This is the first consistent snapshot after any wait for another writer.
func verifySubscriptionOrderScope(tx *gorm.DB, scope subscriptionOrderScope) error {
	var sub model.Subscription
	err := tx.Select("id,start_at").Where("id = ? AND user_id = ?", scope.Subscription, scope.Owner).First(&sub).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !sub.StartAt.Equal(scope.StartAt) {
		return commerce.ErrSubscriptionQuoteChanged
	}
	ids, err := readOrderScopeIDs(terminationOrders(tx, scope.Owner, scope.Subscription, scope.StartAt))
	if err != nil {
		return err
	}
	if len(ids) != len(scope.IDs) {
		return commerce.ErrSubscriptionQuoteChanged
	}
	for i, id := range ids {
		if id != scope.IDs[i] {
			return commerce.ErrSubscriptionQuoteChanged
		}
	}
	return nil
}
