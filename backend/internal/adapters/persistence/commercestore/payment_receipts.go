package commercestore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

type PaymentReceipts struct{ DB *gorm.DB }

func summarizeReceipt(row model.PaymentReceipt, admin bool) commerce.PaymentReceiptSummary {
	out := commerce.PaymentReceiptSummary{ID: row.ID, AmountMinor: row.AmountMinor, Currency: row.Currency, Disposition: row.Disposition, WalletEntryID: row.WalletEntryID, CreatedAt: row.CreatedAt}
	if admin {
		out.Provider, out.ProviderTradeNo = row.Provider, row.ProviderTradeNo
		out.TicketID, out.ResolutionReason = row.TicketID, row.ResolutionReason
	}
	return out
}
func (s PaymentReceipts) Read(ctx context.Context, actor, orderID uint, admin bool, after uint, limit int) (out commerce.PaymentReceiptPage, err error) {
	out.Items = []commerce.PaymentReceiptSummary{}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, admin); err != nil {
			return err
		}
		var order model.Order
		query := tx.Select("id", "user_id").Where("id = ?", orderID)
		if !admin {
			query = query.Where("user_id = ?", actor)
		}
		if err := query.First(&order).Error; err != nil {
			return resourceError(err)
		}
		var rows []model.PaymentReceipt
		query = tx.Where("order_id = ?", orderID)
		if after > 0 {
			query = query.Where("id < ?", after)
		}
		query = query.Omit("receipt_key")
		if !admin {
			query = query.Omit("receipt_key", "provider", "provider_trade_no")
		}
		if err := query.Order("id desc").Limit(limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		out.HasMore = len(rows) > limit
		if out.HasMore {
			rows = rows[:limit]
		}
		for _, row := range rows {
			out.Items = append(out.Items, summarizeReceipt(row, admin))
		}
		return nil
	})
	return
}
func (s PaymentReceipts) Resolve(ctx context.Context, actor, orderID uint, in commerce.ResolveReceiptRequest) (out commerce.PaymentReceiptSummary, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Same financial lock order as payment facts; never receipt before order.
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, orderID).Error; err != nil {
			return resourceError(err)
		}
		admin, err := lockSettlementUsers(tx, order.UserID, actor)
		if err != nil {
			return err
		}
		var receipt model.PaymentReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND order_id = ?", in.ReceiptID, orderID).First(&receipt).Error; err != nil {
			return resourceError(err)
		}
		disposition := "wallet_credit"
		if in.Decision == "already_applied" {
			disposition = "legacy_applied"
		}
		if receipt.ResolvedAt != nil {
			if receipt.Disposition != disposition || receipt.TicketID == nil || *receipt.TicketID != in.TicketID || receipt.ResolutionReason != in.Reason {
				return commerce.ErrPaymentFactConflict
			}
			out = summarizeReceipt(receipt, true)
			return nil
		}
		if receipt.Disposition != "review_required" {
			return commerce.ErrPaymentFactConflict
		}
		var ticket model.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", in.TicketID, order.UserID).First(&ticket).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &commerce.ValidationError{Message: "工单不存在或不属于收据所有者。"}
			}
			return err
		}
		now := time.Now().UTC()
		if in.Decision == "credit_wallet" {
			entryID, err := appendWalletCredit(tx, order, walletKey("payment-receipt:"+strconv.FormatUint(uint64(receipt.ID), 10)), "payment_credit", "管理员核对渠道款项后转入余额", receipt.AmountMinor, now)
			if err != nil {
				return err
			}
			receipt.WalletEntryID = &entryID
		}
		receipt.Disposition, receipt.TicketID, receipt.ResolutionReason, receipt.ResolvedBy, receipt.ResolvedAt = disposition, &ticket.ID, in.Reason, &actor, &now
		if err := tx.Model(&receipt).Updates(map[string]any{"disposition": disposition, "wallet_entry_id": receipt.WalletEntryID, "ticket_id": ticket.ID, "resolution_reason": in.Reason, "resolved_by": actor, "resolved_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: admin.Email, Action: "commerce.payment.resolve", Target: fmt.Sprintf("receipt:%d", receipt.ID), Detail: fmt.Sprintf("%s ticket:%d reason:%s", in.Decision, ticket.ID, in.Reason)}).Error; err != nil {
			return err
		}
		out = summarizeReceipt(receipt, true)
		return nil
	})
	return
}
