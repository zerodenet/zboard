package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

const CheckoutPaymentSlot = "account.checkout.payment"
const OrderPaymentSlot = "account.orders.payment"

var ErrRateLimited = errors.New("plugin request rate limited")

type nativeIdentityKey struct{}

// NativePaymentIdentity comes only from the authenticated process socket.
func NativePaymentIdentity(ctx context.Context) string {
	value, _ := ctx.Value(nativeIdentityKey{}).(string)
	return value
}
func paymentIdentity(v Installation) string {
	digest := sha256.Sum256([]byte(v.ID + "\x00" + v.SigningKey))
	return "plugin:" + hex.EncodeToString(digest[:12])
}
func paymentSlot(name string) bool { return name == CheckoutPaymentSlot || name == OrderPaymentSlot }
func hasPaymentContribution(m Manifest) bool {
	for _, c := range m.Contributions.Slots {
		if c.Surface == "account" && paymentSlot(c.Slot) {
			return true
		}
	}
	return false
}

type PluginCheckoutIssuer interface {
	PluginCheckout(context.Context, string, string, uint, uint) (pluginv1.CheckoutContext, string, error)
}

func (m *Manager) CheckoutContext(ctx context.Context, token string, userID uint, admin bool, orderID uint) (pluginv1.CheckoutContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	out, _, err := m.checkoutLocked(ctx, token, userID, admin, orderID)
	return out, err
}
func (m *Manager) checkoutLocked(ctx context.Context, token string, userID uint, admin bool, orderID uint) (pluginv1.CheckoutContext, string, error) {
	s, err := m.session(token)
	if err != nil {
		return pluginv1.CheckoutContext{}, "", err
	}
	if userID == 0 || s.UserID != userID || s.Purpose != "slot" || s.Surface != "account" || !paymentSlot(s.Slot) || orderID == 0 {
		return pluginv1.CheckoutContext{}, "", ErrPermission
	}
	v, err := m.load(s.PluginID)
	if err != nil {
		return pluginv1.CheckoutContext{}, "", ErrUnavailable
	}
	issuer, ok := m.services.(PluginCheckoutIssuer)
	if !ok || m.processes[v.ID] == nil {
		return pluginv1.CheckoutContext{}, "", ErrUnavailable
	}
	return issuer.PluginCheckout(ctx, v.ID, paymentIdentity(v), userID, orderID)
}
func (m *Manager) SessionCheckoutAction(ctx context.Context, token string, userID uint, admin bool, orderID uint, action string, payload json.RawMessage) (json.RawMessage, error) {
	if orderID == 0 {
		return nil, errors.New("checkout action requires an order")
	}
	return m.sessionAction(ctx, token, userID, admin, orderID, action, payload)
}

// m.mu must be held; native admission survives per-request catalog construction.
func (m *Manager) admitPluginCall(key string, limit int) error {
	if m.capabilityWindows == nil {
		m.capabilityWindows = map[string]capabilityAdmissionWindow{}
	}
	now := time.Now()
	w := m.capabilityWindows[key]
	if now.Sub(w.StartedAt) >= time.Minute {
		w = capabilityAdmissionWindow{StartedAt: now}
	}
	if w.Count >= limit {
		return ErrRateLimited
	}
	if len(m.capabilityWindows) >= 4096 && w.Count == 0 {
		for k, old := range m.capabilityWindows {
			if now.Sub(old.StartedAt) >= time.Minute {
				delete(m.capabilityWindows, k)
			}
		}
		if len(m.capabilityWindows) >= 4096 {
			return ErrUnavailable
		}
	}
	w.Count++
	m.capabilityWindows[key] = w
	return nil
}
