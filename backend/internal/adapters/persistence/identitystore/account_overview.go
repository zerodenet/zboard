package identitystore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func entitlementEnd(alias string) string {
	return fmt.Sprintf("CASE WHEN %s.ended_at IS NOT NULL AND %s.ended_at<%s.end_at THEN %s.ended_at ELSE %s.end_at END", alias, alias, alias, alias, alias)
}

func sharedEntitlementEnd() string {
	end := entitlementEnd("s")
	return "CASE WHEN m.removed_at IS NOT NULL AND m.removed_at<" + end + " THEN m.removed_at ELSE " + end + " END"
}

// One definition for the worker, manual deletion and the displayed estimate.
// The owner expression is always a constant SQL identifier or a bound parameter.
type accountFinancialGuard struct{ table, condition, reason string }

func accountFinancialGuards() []accountFinancialGuard {
	return []accountFinancialGuard{
		{"wallet_accounts", "user_id=%s AND (balance_minor<>0 OR reserved_minor<>0)", "钱包有余额或冻结资金"},
		{"orders", "user_id=%s AND (status='pending' OR (status='paid' AND fulfilled_at IS NULL))", "有未结订单"},
		{"order_fundings", "user_id=%s AND state='reserved'", "有订单资金预留"},
		{"wallet_adjustments", "user_id=%s AND external_status NOT IN ('succeeded','failed')", "有待核对的出款"},
		{"payment_receipts", "order_id IN (SELECT id FROM orders WHERE user_id=%s) AND disposition IN ('processing','review_required')", "有待核对的到账记录"},
		{"subscription_automatic_renewals", "user_id=%s AND pending_order_id IS NOT NULL", "有待处理自动续费"},
	}
}

func (g accountFinancialGuard) exists(owner string) string {
	return "EXISTS (SELECT 1 FROM " + g.table + " WHERE " + fmt.Sprintf(g.condition, owner) + ")"
}

func parseEntitlementEnd(value *string) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999"} {
		if at, err := time.Parse(layout, *value); err == nil {
			return at.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid entitlement end in account overview")
}

// Only the requested page's owners are queried; no traffic/ledger scans, per-row
// requests, persisted countdowns or wallet-creation side effects.
func (s AccountDirectory) overview(ctx context.Context, users []model.User, now time.Time) (map[uint]identity.AccountOverview, error) {
	out := make(map[uint]identity.AccountOverview, len(users))
	if len(users) == 0 {
		return out, nil
	}
	db := s.DB.WithContext(ctx)
	days, err := cleanupDays(db, false)
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
		out[user.ID] = identity.AccountOverview{WalletBalances: []identity.AccountWalletBalance{}, Cleanup: identity.AccountCleanupStatus{Enabled: days > 0, InactiveDays: days, State: "disabled", Reason: "自动删除未启用"}}
	}
	var wallets []model.WalletAccount
	if err := db.Select("user_id,currency,balance_minor,reserved_minor").Where("user_id IN ?", ids).Order("currency ASC").Find(&wallets).Error; err != nil {
		return nil, err
	}
	for _, w := range wallets {
		item := out[w.UserID]
		item.WalletBalances = append(item.WalletBalances, identity.AccountWalletBalance{Currency: w.Currency, BalanceMinor: strconv.FormatInt(w.BalanceMinor, 10), ReservedMinor: strconv.FormatInt(w.ReservedMinor, 10)})
		out[w.UserID] = item
	}
	if days == 0 {
		return out, nil
	}
	selects := []string{"users.id", "(SELECT a.last_entitlement_end FROM account_subjects a WHERE a.id=users.id) AS deleted_end", "(SELECT MAX(" + entitlementEnd("s") + ") FROM subscriptions s WHERE s.user_id=users.id) AS owned_end", "(SELECT MAX(" + sharedEntitlementEnd() + ") FROM subscription_members m JOIN subscriptions s ON s.id=m.subscription_id WHERE m.user_id=users.id) AS shared_end"}
	blocker := "CASE WHEN users.is_admin THEN '管理员账号受保护'"
	for _, g := range accountFinancialGuards() {
		blocker += " WHEN " + g.exists("users.id") + " THEN '" + g.reason + "'"
	}
	selects = append(selects, blocker+" ELSE '' END AS blocker")
	var rows []struct {
		ID                              uint
		OwnedEnd, SharedEnd, DeletedEnd *string
		Blocker                         string
	}
	if err := db.Model(&model.User{}).Select(strings.Join(selects, ", ")).Where("users.id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]model.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	seen := make(map[uint]bool, len(rows))
	for _, row := range rows {
		seen[row.ID] = true
		item := out[row.ID]
		if row.Blocker != "" {
			item.Cleanup.State = "protected"
			item.Cleanup.Reason = row.Blocker
			out[row.ID] = item
			continue
		}
		lastEnd := byID[row.ID].CreatedAt.UTC()
		for _, raw := range []*string{row.OwnedEnd, row.SharedEnd, row.DeletedEnd} {
			at, err := parseEntitlementEnd(raw)
			if err != nil {
				return nil, err
			}
			if at.After(lastEnd) {
				lastEnd = at
			}
		}
		eligible := lastEnd.AddDate(0, 0, days)
		if lastEnd.Year() >= 9999 || eligible.Year() > 9999 {
			item.Cleanup.State = "subscribed"
			item.Cleanup.Reason = "长期订阅尚未结束，结束后重新计算清理时间"
			out[row.ID] = item
			continue
		}
		item.Cleanup.EligibleAt = &eligible
		switch {
		case lastEnd.After(now):
			item.Cleanup.State = "subscribed"
			item.Cleanup.Reason = "订阅尚未结束；未续订时按最近订阅结束时间计算"
		case eligible.After(now):
			item.Cleanup.State = "scheduled"
			item.Cleanup.Reason = "从注册或最后一份订阅结束日起计算"
		default:
			item.Cleanup.State = "eligible"
			item.Cleanup.Reason = "已达到清理条件，等待自动删除；执行前会再次核对订阅和资金状态"
		}
		out[row.ID] = item
	}
	// A concurrently deleted account must not inherit the initialization value
	// (enabled policy with a disabled state) or an invented deadline.
	for id := range out {
		if !seen[id] {
			delete(out, id)
		}
	}
	return out, nil
}
