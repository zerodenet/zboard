package commercestore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const unpricedTerminationKind = "terminate_without_refund"
const unpricedTerminationOrigin = "commerce.admin_zero_unpriced.v1"

type unpricedTerminationEvidence struct {
	Version            int                                           `json:"version"`
	Origin             string                                        `json:"origin"`
	OwnerID            uint                                          `json:"owner_id"`
	ActorID            uint                                          `json:"actor_id"`
	RequestFingerprint string                                        `json:"request_fingerprint"`
	Quote              commerce.AdministrativeSubscriptionValueQuote `json:"quote"`
	ActualMinor        int64                                         `json:"actual_minor,string"`
	Reason             string                                        `json:"reason"`
}

func unpricedTerminationReceipt(row model.SubscriptionMutation) commerce.SubscriptionTerminationReceipt {
	return commerce.SubscriptionTerminationReceipt{ID: row.ID, SubscriptionID: row.SubscriptionID, RecordKind: "entitlement_mutation", AmountMinor: 0, Status: "canceled", RevocationStatus: "pending_verification", CreatedAt: row.CreatedAt}
}

func unpricedTerminationReplay(tx *gorm.DB, row model.SubscriptionMutation, actor, owner, id uint, fp string) (commerce.SubscriptionTerminationReceipt, error) {
	if row.Kind != unpricedTerminationKind || row.Origin != unpricedTerminationOrigin || row.SubscriptionID != id || row.ActorID != actor || row.RequestHash != fp || row.Status != "canceled" {
		return commerce.SubscriptionTerminationReceipt{}, commerce.ErrSubscriptionSettlementConflict
	}
	var event model.QuotaEvent
	if err := tx.Where("subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?", id, "unsubscribe", "subscription_mutation", strconv.FormatUint(uint64(row.ID), 10)).First(&event).Error; err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	var evidence unpricedTerminationEvidence
	if json.Unmarshal([]byte(event.Detail), &evidence) != nil || evidence.Version != 1 || evidence.Origin != unpricedTerminationOrigin || evidence.OwnerID != owner || evidence.ActorID != actor || evidence.RequestFingerprint != fp || evidence.ActualMinor != 0 || evidence.Quote.ValuationStatus != "unavailable" || evidence.Quote.TotalMinor != nil {
		return commerce.SubscriptionTerminationReceipt{}, commerce.ErrSubscriptionSettlementConflict
	}
	return unpricedTerminationReceipt(row), nil
}

// An existing durable entitlement command is the identity for a zero-money
// operation. Do not invent a paid root, a value settlement, or a wallet entry.
func settleUnpricedTermination(tx *gorm.DB, sub model.Subscription, orders []model.Order, admin model.User, actor, owner uint, key, fp string, in commerce.AdministrativeSubscriptionTerminationRequest, quote commerce.AdministrativeSubscriptionValueQuote, now time.Time) (commerce.SubscriptionTerminationReceipt, error) {
	row := model.SubscriptionMutation{SubscriptionID: sub.ID, ActorID: actor, Kind: unpricedTerminationKind, Reason: "管理员明确0元终止；完整原因见关联权益事件", Origin: unpricedTerminationOrigin, IdempotencyKey: key, RequestHash: fp, Status: "canceled", EndAt: sub.EndAt, CreatedAt: now}
	if err := tx.Create(&row).Error; err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	detail := fmt.Sprintf("mutation=%d actor_id=%d owner_id=%d default_minor=unknown actual_minor=0 reason=%q delivery=pending_verification", row.ID, actor, owner, in.Reason)
	reference := strconv.FormatUint(uint64(row.ID), 10)
	if err := finishSubscriptionTermination(tx, sub, orders, admin, actor, "subscription_mutation", reference, detail, now); err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	evidence := unpricedTerminationEvidence{Version: 1, Origin: unpricedTerminationOrigin, OwnerID: owner, ActorID: actor, RequestFingerprint: fp, Quote: quote, ActualMinor: 0, Reason: in.Reason}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	result := tx.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?", sub.ID, "unsubscribe", "subscription_mutation", reference).Update("detail", string(raw))
	if result.Error != nil {
		return commerce.SubscriptionTerminationReceipt{}, result.Error
	}
	if result.RowsAffected != 1 {
		return commerce.SubscriptionTerminationReceipt{}, commerce.ErrSubscriptionSettlementConflict
	}
	var persisted model.SubscriptionMutation
	if err := tx.First(&persisted, row.ID).Error; err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	return unpricedTerminationReceipt(persisted), nil
}
