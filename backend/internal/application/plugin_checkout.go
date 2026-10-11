package application

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type checkoutAuthority struct {
	AccountID uint   `json:"account_id"`
	OrderID   uint   `json:"order_id"`
	Identity  string `json:"identity"`
}

func (s *PluginHostServices) PluginCheckout(ctx context.Context, pluginID, identityID string, accountID, orderID uint) (pluginv1.CheckoutContext, string, error) {
	if s == nil || s.services == nil || s.services.Identity.secret == "" {
		return pluginv1.CheckoutContext{}, "", plugins.ErrUnavailable
	}
	page, err := s.services.OrderQueries.Owned(ctx, accountID, commerce.OrderQuery{ID: orderID, Limit: 1})
	if err != nil {
		if errors.Is(err, commerce.ErrOrderPermission) {
			return pluginv1.CheckoutContext{}, "", plugins.ErrPermission
		}
		return pluginv1.CheckoutContext{}, "", plugins.ErrUnavailable
	}
	if len(page.Items) != 1 {
		return pluginv1.CheckoutContext{}, "", plugins.ErrPermission
	}
	o := page.Items[0]
	if o.Status != "pending" && o.Status != "paid" && o.Status != "failed" {
		return pluginv1.CheckoutContext{}, "", plugins.ErrConflict
	}
	var installation model.Installation
	if err := s.services.Identity.db.WithContext(ctx).First(&installation, 1).Error; err != nil {
		return pluginv1.CheckoutContext{}, "", plugins.ErrUnavailable
	}
	u, err := url.Parse(strings.TrimRight(installation.SiteURL, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" {
		return pluginv1.CheckoutContext{}, "", plugins.ErrUnavailable
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/account/orders"
	u.RawPath = ""
	u.RawQuery = (url.Values{"payment_order": {strconv.FormatUint(uint64(o.ID), 10)}, "payment_plugin": {pluginID}}).Encode()
	payload, _ := json.Marshal(checkoutAuthority{AccountID: accountID, OrderID: orderID, Identity: identityID})
	token := "p2." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(s.principalSignature(pluginID, append([]byte("checkout\x00"), payload...)))
	return pluginv1.CheckoutContext{OrderID: o.ID, TradeNo: o.TradeNo, AccountID: o.UserID, PayableAmount: o.PayableAmount, Currency: o.Currency, CurrencyPrecision: 2, Status: o.Status, ReturnURL: u.String()}, token, nil
}
func (s *PluginHostServices) checkoutPrincipal(ctx context.Context, pluginID, token string) (checkoutAuthority, error) {
	var grant checkoutAuthority
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "p2" || plugins.NativePaymentIdentity(ctx) == "" {
		return grant, plugins.ErrPermission
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return grant, plugins.ErrPermission
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return grant, plugins.ErrPermission
	}
	if !hmac.Equal(sig, s.principalSignature(pluginID, append([]byte("checkout\x00"), payload...))) || json.Unmarshal(payload, &grant) != nil || grant.AccountID == 0 || grant.OrderID == 0 || grant.Identity != plugins.NativePaymentIdentity(ctx) {
		return checkoutAuthority{}, plugins.ErrPermission
	}
	if _, err := s.services.Identity.Accounts.Me(ctx, identity.Principal{ID: grant.AccountID}); err != nil {
		if errors.Is(err, identity.ErrUnavailable) {
			return checkoutAuthority{}, plugins.ErrPermission
		}
		return checkoutAuthority{}, plugins.ErrUnavailable
	}
	return grant, nil
}
