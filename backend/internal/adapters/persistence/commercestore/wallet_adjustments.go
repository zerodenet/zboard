package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

type WalletAdjustments struct{ DB *gorm.DB }

func summarizeWalletAdjustment(row model.WalletAdjustment) commerce.WalletAdjustmentSummary {
	return commerce.WalletAdjustmentSummary{ID: row.ID, UserID: row.UserID, Currency: row.Currency, AmountMinor: row.AmountMinor, Reason: row.Reason, TicketID: row.TicketID, ActorID: row.ActorID, EntryID: row.EntryID, ReversalEntryID: row.ReversalEntryID, ReversalActorID: row.ReversalActorID, ReversalReason: row.ReversalReason, ExternalStatus: row.ExternalStatus, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func (s WalletAdjustments) AdministrativeWallet(ctx context.Context, actor, user uint, currency string, after uint, limit int) (commerce.WalletPage, error) {
	return (Wallet{DB: s.DB}).readFor(ctx, actor, user, true, currency, after, limit)
}
func (s WalletAdjustments) Debit(ctx context.Context, actor uint, in commerce.WalletDebitRequest) (out commerce.WalletAdjustmentSummary, err error) {
	if !creationKeyPattern.MatchString(in.IdempotencyKey) {
		return out, &commerce.ValidationError{Message: "请求标识无效。"}
	}
	key := walletKey("wallet-adjustment:" + in.IdempotencyKey)
	payload, _ := json.Marshal(in)
	fingerprint := walletKey(string(payload))
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var row model.WalletAdjustment
		err := tx.Where("request_key = ?", key).First(&row).Error
		if err == nil {
			if row.Fingerprint != fingerprint {
				return commerce.ErrWalletAdjustmentConflict
			}
			out = summarizeWalletAdjustment(row)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var user model.User
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "SHARE"}).Select("id").First(&user, in.UserID).Error; err != nil {
			return resourceError(err)
		}
		var ticket model.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", in.TicketID, in.UserID).First(&ticket).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &commerce.ValidationError{Message: "工单不存在或不属于目标账户。"}
			}
			return err
		}
		available, err := walletAvailable(tx, in.UserID, in.Currency, true)
		if err != nil {
			return err
		}
		// The account lock serializes identical debit intents too. Recheck using
		// a locking read rather than this transaction's older MySQL snapshot.
		var current model.WalletAdjustment
		again := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_key = ?", key).First(&current).Error
		if again == nil {
			if current.Fingerprint != fingerprint {
				return commerce.ErrWalletAdjustmentConflict
			}
			out = summarizeWalletAdjustment(current)
			return nil
		}
		if !errors.Is(again, gorm.ErrRecordNotFound) {
			return again
		}
		if available < in.AmountMinor {
			return &commerce.ValidationError{Message: "可用余额不足，不能扣除订单预留资金或产生负余额。"}
		}
		now := time.Now().UTC()
		entryID, err := appendWalletEntry(tx, walletEntrySource{UserID: in.UserID, Currency: in.Currency}, walletKey("adjustment-debit:"+key), "admin_debit", fmt.Sprintf("人工扣减：%s（工单#%d）", in.Reason, in.TicketID), -in.AmountMinor, 0, now)
		if err != nil {
			return err
		}
		row = model.WalletAdjustment{RequestKey: key, Fingerprint: fingerprint, UserID: in.UserID, Currency: in.Currency, AmountMinor: in.AmountMinor, Reason: in.Reason, TicketID: in.TicketID, ActorID: actor, EntryID: entryID, ExternalStatus: "pending", CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("admin:%d", actor), Action: "wallet.adjustment.debit", Target: fmt.Sprintf("wallet-adjustment:%d", row.ID), Detail: fmt.Sprintf("user:%d amount:%d currency:%s ticket:%d reason:%s", in.UserID, in.AmountMinor, in.Currency, in.TicketID, in.Reason)}).Error; err != nil {
			return err
		}
		out = summarizeWalletAdjustment(row)
		return nil
	})
	if uniqueViolation(err) || errors.Is(err, commerce.ErrPaymentFactConflict) {
		err = commerce.ErrWalletAdjustmentConflict
	}
	return
}
func (s WalletAdjustments) Read(ctx context.Context, actor, id uint) (out commerce.WalletAdjustmentSummary, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var row model.WalletAdjustment
		if err := tx.First(&row, id).Error; err != nil {
			return resourceError(err)
		}
		out = summarizeWalletAdjustment(row)
		var results []model.WalletPayoutResult
		if err := tx.Where("adjustment_id = ?", id).Order("id desc").Limit(50).Find(&results).Error; err != nil {
			return err
		}
		for _, result := range results {
			out.Results = append(out.Results, commerce.WalletPayoutSummary{ID: result.ID, ActorID: result.ActorID, Outcome: result.Outcome, Reference: result.Reference, Note: result.Note, OccurredAt: result.OccurredAt, CreatedAt: result.CreatedAt})
		}
		return nil
	})
	return
}
func (s WalletAdjustments) List(ctx context.Context, actor, user uint, currency string, after uint, limit int) (out commerce.WalletAdjustmentPage, err error) {
	out.Items = []commerce.WalletAdjustmentSummary{}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var rows []model.WalletAdjustment
		query := tx.Where("user_id = ? AND currency = ?", user, currency)
		if after > 0 {
			query = query.Where("id < ?", after)
		}
		if err := query.Order("id desc").Limit(limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		out.HasMore = len(rows) > limit
		if out.HasMore {
			rows = rows[:limit]
		}
		for _, row := range rows {
			out.Items = append(out.Items, summarizeWalletAdjustment(row))
		}
		return nil
	})
	return
}
func (s WalletAdjustments) RecordPayout(ctx context.Context, actor, id uint, in commerce.WalletPayoutRequest) (out commerce.WalletAdjustmentSummary, err error) {
	if !creationKeyPattern.MatchString(in.IdempotencyKey) {
		return out, &commerce.ValidationError{Message: "请求标识无效。"}
	}
	key := walletKey("wallet-payout-result:" + in.IdempotencyKey)
	payload, _ := json.Marshal(struct {
		ID    uint
		Input commerce.WalletPayoutRequest
	}{id, in})
	fingerprint := walletKey(string(payload))
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var row model.WalletAdjustment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return resourceError(err)
		}
		var prior model.WalletPayoutResult
		err := tx.Where("request_key = ?", key).First(&prior).Error
		if err == nil {
			if prior.AdjustmentID != id || prior.Fingerprint != fingerprint {
				return commerce.ErrWalletAdjustmentConflict
			}
			out = summarizeWalletAdjustment(row)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if (row.ExternalStatus == "succeeded" || row.ExternalStatus == "failed") && row.ExternalStatus != in.Outcome {
			return commerce.ErrWalletAdjustmentConflict
		}
		now := time.Now().UTC()
		record := model.WalletPayoutResult{AdjustmentID: id, RequestKey: key, Fingerprint: fingerprint, ActorID: actor, Outcome: in.Outcome, Reference: in.Reference, Note: in.Note, OccurredAt: in.OccurredAt, CreatedAt: now}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"external_status": in.Outcome, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("admin:%d", actor), Action: "wallet.payout.report", Target: fmt.Sprintf("wallet-adjustment:%d", id), Detail: fmt.Sprintf("result:%s reference:%s", in.Outcome, in.Reference)}).Error; err != nil {
			return err
		}
		row.ExternalStatus, row.UpdatedAt = in.Outcome, now
		out = summarizeWalletAdjustment(row)
		return nil
	})
	if uniqueViolation(err) || errors.Is(err, commerce.ErrPaymentFactConflict) {
		err = commerce.ErrWalletAdjustmentConflict
	}
	return
}
func (s WalletAdjustments) Reverse(ctx context.Context, actor, id uint, in commerce.WalletReversalRequest) (out commerce.WalletAdjustmentSummary, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var row model.WalletAdjustment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return resourceError(err)
		}
		if row.ReversalEntryID != nil {
			out = summarizeWalletAdjustment(row)
			return nil
		}
		if row.ExternalStatus != "failed" {
			return commerce.ErrWalletAdjustmentConflict
		}
		now := time.Now().UTC()
		entryID, err := appendWalletEntry(tx, walletEntrySource{UserID: row.UserID, Currency: row.Currency}, walletKey("adjustment-reversal:"+strconv.FormatUint(uint64(id), 10)), "admin_debit_reversal", fmt.Sprintf("出款确认失败恢复：%s（工单#%d）", in.Reason, row.TicketID), row.AmountMinor, 0, now)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"reversal_entry_id": entryID, "reversal_actor_id": actor, "reversal_reason": in.Reason, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("admin:%d", actor), Action: "wallet.adjustment.reverse", Target: fmt.Sprintf("wallet-adjustment:%d", id), Detail: fmt.Sprintf("ticket:%d reason:%s", row.TicketID, in.Reason)}).Error; err != nil {
			return err
		}
		row.ReversalEntryID, row.ReversalActorID, row.ReversalReason, row.UpdatedAt = &entryID, &actor, in.Reason, now
		out = summarizeWalletAdjustment(row)
		return nil
	})
	return
}
