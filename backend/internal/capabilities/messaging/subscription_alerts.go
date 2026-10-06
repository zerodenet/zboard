package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

type AlertPolicy struct {
	Low, Exhausted, Expiring, Expired             bool
	RemainingPercent, ExpiringDays, IntervalHours int
}

func (p AlertPolicy) Enabled() bool { return p.Low || p.Exhausted || p.Expiring || p.Expired }

type SubscriptionAlertGuard struct {
	SubscriptionID uint   `json:"subscription_id"`
	Kind           string `json:"kind"`
	Episode        string `json:"episode"`
}

type SubscriptionAlertsRepository interface {
	Process(context.Context, time.Time, int) (int, error)
}
type SubscriptionAlerts struct{ Repository SubscriptionAlertsRepository }

func (s SubscriptionAlerts) Process(ctx context.Context) (int, error) {
	return s.Repository.Process(ctx, time.Now().UTC(), 200)
}

// Priority applies within a subscription. Frequency control applies to the
// account across all its subscriptions. Historical expiry is limited to the
// renewal grace period, so enabling alerts does not mail years-old accounts.
func AlertCandidates(sub entitlements.Subscription, p AlertPolicy, now time.Time) []SubscriptionAlertGuard {
	if (sub.Status != "active" && sub.Status != "expired") || sub.StartAt.After(now) {
		return nil
	}
	guard := func(kind, episode string) SubscriptionAlertGuard {
		return SubscriptionAlertGuard{SubscriptionID: sub.ID, Kind: kind, Episode: episode}
	}
	expiry := sub.EndAt.UTC().Format(time.RFC3339Nano)
	if !entitlements.IsPerpetualEnd(sub.EndAt) && !sub.EndAt.After(now) {
		if p.Expired && sub.EndAt.After(now.Add(-entitlements.RenewalGracePeriod)) && (sub.EndedAt == nil || sub.EndReason == "expired") {
			return []SubscriptionAlertGuard{guard("expired", expiry)}
		}
		return nil
	}
	if sub.EndedAt != nil && sub.EndReason != "exhausted" {
		return nil
	}
	total, used := entitlements.CycleQuota(sub)
	cycle := fmt.Sprintf("%s/%d", sub.StartAt.UTC().Format(time.RFC3339Nano), sub.CycleStartUsed)
	nextReset := sub.NextResetAt
	if nextReset == nil && sub.ResetPolicy >= 1 && sub.ResetPolicy <= 4 {
		nextReset = entitlements.NextTrafficReset(sub.StartAt, sub.ResetPolicy)
	}
	if nextReset != nil {
		cycle += "/" + nextReset.UTC().Format(time.RFC3339Nano)
	}
	var out []SubscriptionAlertGuard
	if p.Exhausted && total > 0 && used >= total {
		out = append(out, guard("exhausted", cycle))
	}
	if p.Expiring && sub.EndedAt == nil && !entitlements.IsPerpetualEnd(sub.EndAt) && sub.EndAt.Sub(now) <= time.Duration(p.ExpiringDays)*24*time.Hour {
		out = append(out, guard("expiring", expiry))
	}
	// Integer comparison without multiplying an int64 quota by 100.
	threshold := total/100*int64(p.RemainingPercent) + total%100*int64(p.RemainingPercent)/100
	if p.Low && sub.EndedAt == nil && total > 0 && used >= 0 && used < total && total-used <= threshold {
		out = append(out, guard("low", cycle))
	}
	return out
}

func AlertContent(sub entitlements.Subscription, guard SubscriptionAlertGuard) EmailContent {
	titles := map[string]string{"low": "订阅流量余量不足", "exhausted": "订阅流量已耗尽", "expiring": "订阅即将到期", "expired": "订阅已到期"}
	body := fmt.Sprintf("{{account_name}}，您好：\n\n您的订阅 #%d：%s。", sub.ID, titles[guard.Kind])
	if guard.Kind == "low" || guard.Kind == "exhausted" {
		total, used := entitlements.CycleQuota(sub)
		body += fmt.Sprintf("\n本周期已使用 %.2f GiB / %.2f GiB，剩余 %.2f GiB。", float64(used)/(1<<30), float64(total)/(1<<30), float64(max(int64(0), total-used))/(1<<30))
	} else {
		body += "\n到期时间：" + sub.EndAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	body += "\n\n请登录 {{site_name}} 查看订阅及续费信息：{{site_url}}\n此邮件为订阅状态提醒，不会改变套餐或网络连接。"
	return EmailContent{Subject: "{{site_name}} · " + titles[guard.Kind], Body: body, Alert: &guard}
}
