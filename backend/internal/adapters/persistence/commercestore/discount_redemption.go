package commercestore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrderDiscountRedemptionRequirement reads only a strict accepted snapshot. The
// presence and kind of required external hold cannot be inferred from coupon
// marketing terms, selected browser inputs or paid-looking legacy fields.
func OrderDiscountRedemptionRequirement(order model.Order) (bool, *commerce.PurchaseDiscountSnapshot, error) {
	if order.PurchaseSnapshot == "" {
		return false, nil, nil
	}
	var header struct {
		Discount *commerce.PurchaseDiscountSnapshot `json:"discount"`
	}
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &header) != nil {
		if !strings.Contains(order.PurchaseSnapshot, `"redemption"`) {
			return false, nil, nil
		} // preserve unrelated unknown historical compatibility
		return false, nil, commerce.ErrDiscountRedemptionConflict
	}
	if header.Discount == nil || header.Discount.Quote.Offer.Redemption == "" {
		return false, nil, nil
	}
	policy, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil || commerce.ValidatePurchaseDiscount(commerce.Order(order), policy) != nil || policy.Discount.Quote.Offer.Redemption != commerce.DiscountRedemptionGuarantee {
		return false, nil, commerce.ErrDiscountRedemptionConflict
	}
	return true, policy.Discount, nil
}

func RequireOrderDiscountReservation(tx *gorm.DB, order model.Order, clocks ...time.Time) error {
	required, _, err := OrderDiscountRedemptionRequirement(order)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	if err := RequireDiscountReservation(tx, order, true, clocks...); err != nil {
		return discountReservationValidation(err)
	}
	return nil
}

func CreateOrderDiscountRedemptionIntent(tx *gorm.DB, order model.Order, now time.Time) error {
	required, accepted, err := OrderDiscountRedemptionRequirement(order)
	if err != nil || !required {
		return err
	}
	return CreateDiscountRedemptionIntent(tx, order, *accepted, now)
}

// All exported transaction helpers require the caller to hold the order UPDATE
// lock first. The one financial path retains its existing user/wallet lock order.
// Provider RPC never occurs in these helpers or under a database transaction.
func CreateDiscountRedemptionIntent(tx *gorm.DB, order model.Order, accepted commerce.PurchaseDiscountSnapshot, now time.Time) error {
	if order.ID == 0 || order.Status != "pending" || accepted.Quote.Offer.Status != "available" || accepted.Quote.Offer.Agreement == nil ||
		accepted.FinalPriceMinor != order.PayableAmount || accepted.Quote.Offer.DiscountMinor != order.DiscountAmount || accepted.Context.BuyerID != order.UserID {
		return commerce.ErrDiscountRedemptionConflict
	}
	var existing model.OrderDiscountRedemption
	err := loadDiscountRedemption(tx, order.ID, false, &existing)
	if err == nil {
		b, err := decodeRedemptionBinding(existing)
		if err != nil || b.OrderID != order.ID || b.BuyerID != order.UserID || b.PayableMinor != order.PayableAmount || b.InputFingerprint != accepted.Context.InputFingerprint || b.TermsHash != accepted.Quote.Offer.Agreement.TermsHash || b.Provider != accepted.Quote.Provider {
			return commerce.ErrDiscountRedemptionConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	key, err := redemptionRandomKey()
	if err != nil {
		return err
	}
	a := accepted.Quote.Offer.Agreement
	b := commerce.DiscountRedemptionBinding{Version: 1, Key: key, OrderID: order.ID, BuyerID: order.UserID, InputFingerprint: accepted.Context.InputFingerprint, Provider: accepted.Quote.Provider, AgreementID: a.ID, AgreementVersion: a.Version, TermsHash: a.TermsHash, BaseMinor: order.AmountCents, DiscountMinor: order.DiscountAmount, PayableMinor: order.PayableAmount, Currency: order.Currency}
	hash, err := commerce.DiscountRedemptionHash(b)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	row := model.OrderDiscountRedemption{OrderID: order.ID, RedemptionKey: key, BindingHash: hash, BindingJSON: string(raw), ProviderID: b.Provider.PluginID, Desired: "reserve", Observed: "unknown", Revision: 1, LeaseUntil: now, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	return tx.Create(&row).Error
}

// required comes from a strict, versioned accepted snapshot marker, never page
// fields or arbitrary plugin TermsJSON. Legacy v1 pure quotes pass false.
func RequireDiscountReservation(tx *gorm.DB, order model.Order, required bool, clocks ...time.Time) error {
	var row model.OrderDiscountRedemption
	err := loadDiscountRedemption(tx, order.ID, true, &row)
	if errors.Is(err, gorm.ErrRecordNotFound) && !required {
		return nil
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return commerce.ErrDiscountRedemptionPending
		}
		return err
	}
	if err := validateDiscountReservationOrder(row, order); err != nil {
		return err
	}
	if row.Token == "" && row.LastError == "provider_redemption_unsupported" {
		return commerce.ErrDiscountRedemptionUnsupported
	}
	if discountReservationCanPay(row, order) {
		now := time.Now().UTC()
		if len(clocks) > 0 {
			now = clocks[0]
		}
		open, err := discountReservationWindowOpen(tx, row, order, now)
		if err != nil {
			return err
		}
		if open {
			return nil
		}
	}
	return commerce.ErrDiscountRedemptionPending
}

// This must be called in the payment transaction, after the reservation gate
// and before its commit. A remote outage cannot change this order's price.
func CommitDiscountRedemptionIntent(tx *gorm.DB, orderID uint, now time.Time) error {
	var row model.OrderDiscountRedemption
	err := loadDiscountRedemption(tx, orderID, true, &row)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Desired == "commit" {
		return nil
	}
	if row.Desired != "reserve" || row.Observed != "reserved" || row.Token == "" {
		return commerce.ErrDiscountRedemptionPending
	}
	return tx.Model(&row).Updates(map[string]any{"desired": "commit", "revision": row.Revision + 1, "next_attempt_at": now, "updated_at": now}).Error
}

// Call only after order payability is atomically closed (cancel/failed/expired).
// A paid order cannot release a reservation; monetary refund is independent.
func ReleaseDiscountRedemptionIntent(tx *gorm.DB, order model.Order, now time.Time) error {
	var row model.OrderDiscountRedemption
	err := loadDiscountRedemption(tx, order.ID, true, &row)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Desired == "release" {
		return nil
	}
	if order.Status == "paid" || row.Desired == "commit" || (order.Status != "canceled" && order.Status != "failed" && order.Status != "expired") {
		return commerce.ErrDiscountRedemptionConflict
	}
	return tx.Model(&row).Updates(map[string]any{"desired": "release", "revision": row.Revision + 1, "next_attempt_at": now, "updated_at": now}).Error
}

func decodeRedemptionBinding(row model.OrderDiscountRedemption) (commerce.DiscountRedemptionBinding, error) {
	var b commerce.DiscountRedemptionBinding
	decoder := json.NewDecoder(bytes.NewBufferString(row.BindingJSON))
	decoder.DisallowUnknownFields()
	if len(row.BindingJSON) == 0 || len(row.BindingJSON) > commerce.MaxDiscountRedemptionBytes || decoder.Decode(&b) != nil || decoder.Decode(new(any)) != io.EOF {
		return b, commerce.ErrDiscountRedemptionConflict
	}
	hash, err := commerce.DiscountRedemptionHash(b)
	if err != nil || hash != row.BindingHash || b.Key != row.RedemptionKey || b.OrderID != row.OrderID || b.Provider.PluginID != row.ProviderID {
		return b, commerce.ErrDiscountRedemptionConflict
	}
	return b, nil
}
func redemptionRandomKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// DiscountRedemptionJobs is the same executor for inline/independent workers.
// A conditional lease bounds duplicate work; external idempotency and Query are
// still required because the process may crash after RPC and before persistence.
type DiscountRedemptionJobs struct {
	DB       *gorm.DB
	Provider commerce.DiscountRedemptionProvider
	Now      func() time.Time
}

func (s DiscountRedemptionJobs) Step(ctx context.Context, orderID uint) error {
	if s.DB == nil || s.Provider == nil {
		return commerce.ErrDiscountRedemptionUnsupported
	}
	now := commerceNow(s.Now)
	var row model.OrderDiscountRedemption
	if err := loadDiscountRedemption(s.DB.WithContext(ctx), orderID, false, &row); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) && ctx.Err() == nil {
			retry := s.DB.WithContext(ctx).Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", orderID).Updates(map[string]any{"next_attempt_at": now.Add(time.Minute), "last_error": "authority_reconciliation_required", "updated_at": now})
			return errors.Join(err, retry.Error)
		}
		return err
	}
	if !row.NextAttemptAt.Before(now.Add(time.Millisecond)) || row.LeaseUntil.After(now) {
		return nil
	}
	if (row.Desired == "commit" && row.Observed == "committed") || (row.Desired == "release" && row.Observed == "released") {
		return nil
	}
	lease, err := redemptionRandomKey()
	if err != nil {
		return err
	}
	claimed := s.DB.WithContext(ctx).Model(&model.OrderDiscountRedemption{}).Where("order_id = ? AND revision = ? AND lease_until <= ?", row.OrderID, row.Revision, now).Updates(map[string]any{"lease_token": lease, "lease_until": now.Add(15 * time.Second), "updated_at": now})
	if claimed.Error != nil {
		return claimed.Error
	}
	if claimed.RowsAffected != 1 {
		return nil
	}
	// Query comes before every mutation and every retry, including first Reserve.
	// Never replace the key following a timeout or a lost confirmation.
	b, err := decodeRedemptionBinding(row)
	if err != nil {
		return s.finish(ctx, row.OrderID, lease, "", "", err)
	}
	query := commerce.DiscountRedemptionRequest{Version: 1, Action: "query", Binding: b, BindingHash: row.BindingHash, Token: row.Token}
	observed, err := s.Provider.RedeemDiscount(ctx, query)
	if err != nil {
		return s.finish(ctx, row.OrderID, lease, "", "", err)
	}
	if err := commerce.ValidateDiscountRedemptionResult(query, observed); err != nil {
		return s.finish(ctx, row.OrderID, lease, "", "", err)
	}
	// Re-read desired after Query: cancellation/payment may have committed while
	// RPC ran. No financial locks survive the external call.
	var current model.OrderDiscountRedemption
	if err := loadDiscountRedemption(s.DB.WithContext(ctx), row.OrderID, false, &current); err != nil {
		return err
	}
	// Once confirmed, disappearance is a provider durability violation, not
	// permission to recreate inventory. Keep the original proof token forever.
	if current.Token != "" && current.Desired != "release" && (observed.State == "absent" || observed.State == "rejected" || observed.State == "released") {
		return s.finish(ctx, row.OrderID, lease, "conflict", current.Token, commerce.ErrDiscountRedemptionConflict)
	}
	if current.LeaseToken != lease {
		return commerce.ErrDiscountRedemptionConflict
	}
	action, err := commerce.NextDiscountRedemptionAction(current.Desired, observed.State)
	if err != nil {
		return s.finish(ctx, row.OrderID, lease, observed.State, observed.Token, err)
	}
	if action == "" {
		return s.finish(ctx, row.OrderID, lease, observed.State, observed.Token, nil)
	}
	command := query
	command.Action = action
	command.Token = observed.Token
	result, err := s.Provider.RedeemDiscount(ctx, command)
	if err != nil {
		return s.finish(ctx, row.OrderID, lease, "", "", err)
	}
	if err := commerce.ValidateDiscountRedemptionResult(command, result); err != nil {
		return s.finish(ctx, row.OrderID, lease, "", "", err)
	}
	return s.finish(ctx, row.OrderID, lease, result.State, result.Token, nil)
}

func (s DiscountRedemptionJobs) finish(ctx context.Context, id uint, lease, observed, token string, failure error) error {
	now := commerceNow(s.Now)
	values := map[string]any{"lease_token": "", "lease_until": now, "next_attempt_at": now.Add(10 * time.Second), "updated_at": now, "last_error": ""}
	if observed != "" {
		values["observed"] = observed
		if token != "" {
			values["token"] = token
		}
	}
	if failure == nil && observed == "reserved" {
		values["next_attempt_at"] = now.Add(time.Minute)
	}
	if failure != nil {
		values["last_error"] = "provider_reconciliation_required"
		if errors.Is(failure, commerce.ErrDiscountRedemptionUnsupported) {
			values["last_error"] = "provider_redemption_unsupported"
		}
	}
	// Do not overwrite desired/revision changed by financial transaction.
	r := s.DB.WithContext(ctx).Model(&model.OrderDiscountRedemption{}).Where("order_id = ? AND lease_token = ?", id, lease).Updates(values)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return commerce.ErrDiscountRedemptionConflict
	}
	return failure
}

func (s DiscountRedemptionJobs) RunBatch(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	_, expiryFailures := s.ExpireUnpaid(ctx, limit)
	now := commerceNow(s.Now)
	var ids []uint
	err := s.DB.WithContext(ctx).Model(&model.OrderDiscountRedemption{}).Select("order_id").Where("next_attempt_at <= ? AND lease_until <= ? AND NOT ((desired = 'commit' AND observed = 'committed') OR (desired = 'release' AND observed = 'released'))", now, now).Order("next_attempt_at, order_id").Limit(limit).Pluck("order_id", &ids).Error
	if err != nil {
		return 0, err
	}
	failures := expiryFailures
	for _, id := range ids {
		if ctx.Err() != nil {
			return 0, errors.Join(failures, ctx.Err())
		}
		failures = errors.Join(failures, s.Step(ctx, id))
	}
	return len(ids), failures
}

// Thin metadata is read before any unbounded SQLite TEXT material is returned to
// Go. The bounded full SELECT repeats the length predicate, covering a change
// between the metadata and full-row reads even outside a financial transaction.
func loadDiscountRedemption(db *gorm.DB, id uint, lock bool, out *model.OrderDiscountRedemption) error {
	size := "OCTET_LENGTH(binding_json)"
	tokenSize := "OCTET_LENGTH(token)"
	if db.Dialector.Name() == "sqlite" {
		size = "length(CAST(binding_json AS BLOB))"
		tokenSize = "length(CAST(token AS BLOB))"
	}
	var meta struct {
		OrderID      uint
		BindingBytes int64
		TokenBytes   int64
	}
	query := db.Model(&model.OrderDiscountRedemption{}).Select("order_id, "+size+" AS binding_bytes, "+tokenSize+" AS token_bytes").Where("order_id = ?", id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(&meta).Error; err != nil {
		return err
	}
	if meta.BindingBytes <= 0 || meta.BindingBytes > commerce.MaxDiscountRedemptionBytes || meta.TokenBytes > 512 {
		return commerce.ErrDiscountRedemptionConflict
	}
	return db.Where("order_id = ? AND "+size+" <= ? AND "+tokenSize+" <= ?", id, commerce.MaxDiscountRedemptionBytes, 512).First(out).Error
}

func validateDiscountReservationOrder(row model.OrderDiscountRedemption, order model.Order) error {
	b, err := decodeRedemptionBinding(row)
	if err != nil || b.OrderID != order.ID || b.BuyerID != order.UserID || b.BaseMinor != order.AmountCents || b.PayableMinor != order.PayableAmount || b.DiscountMinor != order.DiscountAmount || b.Currency != order.Currency {
		return commerce.ErrDiscountRedemptionConflict
	}
	required, accepted, err := OrderDiscountRedemptionRequirement(order)
	if err != nil {
		return err
	}
	if required {
		a := accepted.Quote.Offer.Agreement
		if b.InputFingerprint != accepted.Context.InputFingerprint || b.Provider != accepted.Quote.Provider || b.AgreementID != a.ID || b.AgreementVersion != a.Version || b.TermsHash != a.TermsHash {
			return commerce.ErrDiscountRedemptionConflict
		}
	}
	return nil
}
func discountReservationCanPay(row model.OrderDiscountRedemption, order model.Order) bool {
	return (order.Status == "pending" || order.Status == "failed") && row.Desired == "reserve" && row.Observed == "reserved" && row.Token != ""
}

func discountReservationWindowOpen(tx *gorm.DB, row model.OrderDiscountRedemption, order model.Order, now time.Time) (bool, error) {
	if row.CreatedAt.Add(orderFundingLifetime).After(now) {
		return true, nil
	}
	funding, exists, err := findOrderFunding(tx, order)
	if err != nil {
		return false, err
	}
	return exists && funding.State == "reserved" && funding.ExpiresAt.After(now), nil
}
