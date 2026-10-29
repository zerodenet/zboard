package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const legacyCancellationKind = "cancel_without_refund"
const legacyCancellationEvidenceOrigin = "commerce.legacy_cancel_zero.v1"

type legacyCancellationCommand struct {
	Input   entitlements.SubscriptionMutationInput
	Receipt entitlements.SubscriptionMutationReceipt
}
type legacyCancellationEvidence struct {
	Version       int                                           `json:"version"`
	Origin        string                                        `json:"origin"`
	OwnerID       uint                                          `json:"owner_id"`
	ActorID       uint                                          `json:"actor_id"`
	RequestHash   string                                        `json:"request_hash"`
	CommandOrigin string                                        `json:"command_origin"`
	Reason        string                                        `json:"reason"`
	ActualMinor   int64                                         `json:"actual_minor,string"`
	Quote         commerce.AdministrativeSubscriptionValueQuote `json:"quote"`
}

func legacyCommandReceipt(row model.SubscriptionMutation) entitlements.SubscriptionMutationReceipt {
	return entitlements.SubscriptionMutationReceipt{MutationID: row.ID, SubscriptionID: row.SubscriptionID, Status: row.Status, EndAt: row.EndAt}
}

// No financial transaction or lock is held across this composition preflight.
// status.cancel retains its old no-refund administrator contract, while actual
// termination and source/hold closure belong to the one commerce transaction.
func (s SubscriptionTermination) CancelEntitlement(ctx context.Context, actor uint, in entitlements.SubscriptionMutationInput) (entitlements.SubscriptionMutationReceipt, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	var empty entitlements.SubscriptionMutationReceipt
	if s.DB == nil {
		return empty, entitlements.ErrSubscriptionMutationUnavailable
	}
	if actor == 0 {
		return empty, commerce.ErrOrderPermission
	}
	if in.Kind != entitlements.SubscriptionCancel || in.Days != 0 || in.SubscriptionID == 0 || len(in.Reason) < 3 || len(in.Reason) > 255 || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 128 || len(in.Origin) > 191 {
		return empty, entitlements.ErrSubscriptionMutationInvalid
	}
	command := legacyCancellationCommand{Input: in}
	// Preserve old immutable receipts even after an operator retires the entity.
	// A new-format receipt requires its complete retained unsubscribe evidence.
	var found bool
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var err error
		found, err = lookupLegacyCancellation(tx, actor, &command)
		return err
	})
	if err != nil || found {
		return command.Receipt, err
	}
	var sub model.Subscription
	if err := s.DB.WithContext(ctx).Select("id", "user_id").First(&sub, in.SubscriptionID).Error; err != nil {
		return empty, resourceError(err)
	}
	zero := int64(0)
	request := commerce.SubscriptionTerminationRequest{IdempotencyKey: "legacy_cancel_" + walletKey(in.IdempotencyKey)}
	administrative := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: request, AmountMinor: &zero, Reason: in.Reason}
	_, err = s.terminate(ctx, actor, sub.UserID, sub.ID, request, &administrative, &command)
	if err != nil {
		return empty, err
	}
	return command.Receipt, nil
}

// Caller has checked current administrator authority. This is an exact legacy
// command key/hash, not a receipt guessed from the current subscription state.
func lookupLegacyCancellation(tx *gorm.DB, actor uint, command *legacyCancellationCommand) (bool, error) {
	in := command.Input
	var row model.SubscriptionMutation
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("idempotency_key = ?", in.IdempotencyKey).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if row.ActorID != actor || row.SubscriptionID != in.SubscriptionID || row.RequestHash != entitlements.SubscriptionMutationFingerprint(in) || row.Days != 0 || row.Reason != in.Reason || row.Origin != in.Origin || row.Status != "canceled" {
		return false, commerce.ErrSubscriptionSettlementConflict
	}
	switch row.Kind {
	case entitlements.SubscriptionCancel:
		// Historical status-only cancellation is returned as history. Replaying it
		// cannot silently perform new lifecycle or monetary effects.
	case legacyCancellationKind:
		var event model.QuotaEvent
		if err := tx.Where("subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?", row.SubscriptionID, "unsubscribe", "subscription_mutation", strconv.FormatUint(uint64(row.ID), 10)).First(&event).Error; err != nil {
			return false, err
		}
		var evidence legacyCancellationEvidence
		if json.Unmarshal([]byte(event.Detail), &evidence) != nil || evidence.Version != 1 || evidence.Origin != legacyCancellationEvidenceOrigin || evidence.OwnerID == 0 || evidence.ActorID != actor || evidence.RequestHash != row.RequestHash || evidence.CommandOrigin != in.Origin || evidence.Reason != in.Reason || evidence.ActualMinor != 0 || evidence.Quote.SubscriptionID != in.SubscriptionID || evidence.Quote.UserID != evidence.OwnerID {
			return false, commerce.ErrSubscriptionSettlementConflict
		}
		if evidence.Quote.ValuationStatus != "verified" && evidence.Quote.ValuationStatus != "unavailable" {
			return false, commerce.ErrSubscriptionSettlementConflict
		}
		q := evidence.Quote
		if len(q.Fingerprint) != 64 {
			return false, commerce.ErrSubscriptionSettlementConflict
		}
		if q.ValuationStatus == "unavailable" && (q.TotalMinor != nil || q.CurrentMinor != nil || q.FutureMinor != nil || q.RootOrderID != nil || q.Currency != "" || q.MaxRefundMinor != 0) || q.ValuationStatus == "verified" && (q.TotalMinor == nil || q.CurrentMinor == nil || q.FutureMinor == nil || q.RootOrderID == nil || *q.RootOrderID == 0 || q.Currency == "" || q.MaxRefundMinor < 0) {
			return false, commerce.ErrSubscriptionSettlementConflict
		}
		if q.ValuationStatus == "verified" && (*q.TotalMinor < 0 || *q.CurrentMinor < 0 || *q.FutureMinor < 0 || *q.CurrentMinor > *q.TotalMinor || *q.TotalMinor-*q.CurrentMinor != *q.FutureMinor) {
			return false, commerce.ErrSubscriptionSettlementConflict
		}
	default:
		return false, commerce.ErrSubscriptionSettlementConflict
	}
	command.Receipt = legacyCommandReceipt(row)
	return true, nil
}

func settleLegacyCancellation(tx *gorm.DB, sub model.Subscription, orders []model.Order, admin model.User, actor, owner uint, command *legacyCancellationCommand, quote commerce.AdministrativeSubscriptionValueQuote, now time.Time) (commerce.SubscriptionTerminationReceipt, error) {
	in := command.Input
	if in.Kind != entitlements.SubscriptionCancel || in.Days != 0 || quote.SubscriptionID != sub.ID || quote.UserID != owner {
		return commerce.SubscriptionTerminationReceipt{}, entitlements.ErrSubscriptionMutationInvalid
	}
	row := model.SubscriptionMutation{SubscriptionID: sub.ID, ActorID: actor, Kind: legacyCancellationKind, Days: 0, Reason: in.Reason, Origin: in.Origin, IdempotencyKey: in.IdempotencyKey, RequestHash: entitlements.SubscriptionMutationFingerprint(in), Status: "canceled", EndAt: sub.EndAt, CreatedAt: now}
	if err := tx.Create(&row).Error; err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	defaultMinor := "unknown"
	if quote.TotalMinor != nil {
		defaultMinor = strconv.FormatInt(*quote.TotalMinor, 10)
	}
	detail := fmt.Sprintf("mutation=%d actor_id=%d owner_id=%d default_minor=%s actual_minor=0 origin=%q reason=%q delivery=pending_verification", row.ID, actor, owner, defaultMinor, in.Origin, in.Reason)
	reference := strconv.FormatUint(uint64(row.ID), 10)
	if err := finishSubscriptionTermination(tx, sub, orders, admin, actor, "subscription_mutation", reference, detail, now); err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	evidence := legacyCancellationEvidence{Version: 1, Origin: legacyCancellationEvidenceOrigin, OwnerID: owner, ActorID: actor, RequestHash: row.RequestHash, CommandOrigin: in.Origin, Reason: in.Reason, ActualMinor: 0, Quote: quote}
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
	// Keep the old searchable command audit alongside the core whole-stop audit.
	audit, _ := json.Marshal(map[string]any{"mutation_id": row.ID, "reason": in.Reason, "origin": in.Origin, "actual_minor": "0", "valuation_status": quote.ValuationStatus})
	if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: admin.Email, Action: "subscription." + entitlements.SubscriptionCancel, Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: string(audit)}).Error; err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	var persisted model.SubscriptionMutation
	if err := tx.First(&persisted, row.ID).Error; err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	command.Receipt = legacyCommandReceipt(persisted)
	return commerce.SubscriptionTerminationReceipt{ID: row.ID, SubscriptionID: sub.ID, RecordKind: "entitlement_mutation", Currency: quote.Currency, AmountMinor: 0, Status: "canceled", RevocationStatus: "pending_verification", CreatedAt: persisted.CreatedAt}, nil
}
