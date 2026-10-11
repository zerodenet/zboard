// Signed acceptance plugin. It uses only the public native SDK, private
// storage, and a local simulated provider; no host DB or internal HTTP access.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

var pluginID = "example.payment-one"
var capabilities = []string{pluginv1.CommerceOrdersReadCapability, pluginv1.CommercePaymentsWriteCapability, "zboard.ui.page.v1", "zboard.ui.slot.v1", "zboard.config.v1", "zboard.storage.v1", "zboard.http.route.v1"}

type server struct {
	pluginv1.UnimplementedPluginControlServer
	mu       sync.RWMutex
	provider string
}
type saved struct {
	Actor   string                   `json:"actor"`
	Context pluginv1.CheckoutContext `json:"context"`
}
type outcome struct {
	Paid       bool      `json:"paid"`
	EventID    string    `json:"event_id"`
	TradeNo    string    `json:"trade_no"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: pluginID, Version: "1.0.0", Protocol: 1, Capabilities: capabilities}, nil
}
func (*server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (*server) ValidateConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	var v map[string]string
	if json.Unmarshal(r.ConfigJson, &v) != nil {
		return nil, errors.New("invalid config")
	}
	return &pluginv1.ConfigResult{NormalizedJson: r.ConfigJson}, nil
}
func (s *server) ApplyConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	var v map[string]string
	json.Unmarshal(r.ConfigJson, &v)
	s.mu.Lock()
	s.provider = v["provider"]
	s.mu.Unlock()
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (s *server) HandlePageAction(ctx context.Context, r *pluginv1.PageActionRequest) (*pluginv1.PageActionResponse, error) {
	if r.Purpose != "slot" || r.SlotId == "" || r.PageId != "" {
		return nil, errors.New("wrong contribution context")
	}
	c, err := r.Checkout()
	if err != nil {
		return nil, err
	}
	host, err := pluginv1.HostClientFromEnvironment()
	if err != nil {
		return nil, err
	}
	defer host.Close()
	if r.Action == "mock.read" {
		in := pluginv1.CommerceOrdersRequest{PrincipalID: r.ActorId, ID: c.OrderID, Limit: 1}
		json.Unmarshal(r.PayloadJson, &in)
		// A plugin cannot create authority by claiming a numeric user or principal.
		var out any
		err = listOrders(ctx, host, in, &out)
		return response(out, err)
	}
	if r.Action == "mock.fact" {
		var in pluginv1.PaymentFactRequest
		json.Unmarshal(r.PayloadJson, &in)
		if in.PrincipalID == "" {
			in.PrincipalID = r.ActorId
		}
		out, err := recordPayment(ctx, host, in)
		return response(out, err)
	}
	storage, err := pluginv1.HostStorageFromEnvironment()
	if err != nil {
		return nil, err
	}
	defer storage.Close()
	key := "attempt-" + strconv.FormatUint(uint64(c.OrderID), 10)
	if r.Action == "mock.prepare" {
		var orders any
		if err := listOrders(ctx, host, pluginv1.CommerceOrdersRequest{PrincipalID: r.ActorId, ID: c.OrderID, Limit: 1}, &orders); err != nil {
			return nil, err
		}
		value, err := storageGet(ctx, storage, key)
		if err != nil {
			return nil, err
		}
		if !value.Found {
			raw, _ := json.Marshal(saved{Actor: r.ActorId, Context: c})
			if _, err := storage.Put(ctx, key, value.Revision, raw); err != nil {
				return nil, err
			}
		}
		return response(map[string]any{"checkout_url": fmt.Sprintf("https://cashier.example.test/pay/%d", c.OrderID), "context": c, "orders": orders}, nil)
	}
	if r.Action == "mock.reconcile" {
		return s.reconcile(ctx, c.OrderID)
	}
	return nil, errors.New("unknown plugin action")
}
func (s *server) reconcile(ctx context.Context, id uint) (*pluginv1.PageActionResponse, error) {
	storage, err := pluginv1.HostStorageFromEnvironment()
	if err != nil {
		return nil, err
	}
	defer storage.Close()
	value, err := storageGet(ctx, storage, "attempt-"+strconv.FormatUint(uint64(id), 10))
	if err != nil || !value.Found {
		return nil, errors.New("no authorized attempt")
	}
	var attempt saved
	if json.Unmarshal(value.Value, &attempt) != nil {
		return nil, errors.New("invalid attempt")
	}
	s.mu.RLock()
	provider := s.provider
	s.mu.RUnlock()
	request, err := http.NewRequestWithContext(ctx, "GET", provider+"/orders/"+strconv.FormatUint(uint64(id), 10), nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var state outcome
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil || json.Unmarshal(raw, &state) != nil {
		return nil, errors.New("invalid provider result")
	}
	if !state.Paid {
		return response(map[string]any{"status": "pending"}, nil)
	}
	host, err := pluginv1.HostClientFromEnvironment()
	if err != nil {
		return nil, err
	}
	defer host.Close()
	var order any
	if err := listOrders(ctx, host, pluginv1.CommerceOrdersRequest{PrincipalID: attempt.Actor, ID: id, Limit: 1}, &order); err != nil {
		return nil, err
	}
	fact := pluginv1.PaymentFactRequest{PrincipalID: attempt.Actor, OrderID: id, ProviderEventID: state.EventID, ProviderTradeNo: state.TradeNo, Status: "paid", AmountMinor: attempt.Context.PayableAmount, Currency: attempt.Context.Currency, OccurredAt: state.OccurredAt}
	out, err := recordPayment(ctx, host, fact)
	return response(out, err)
}
func (s *server) HandleHTTP(ctx context.Context, r *pluginv1.HTTPRequest) (*pluginv1.HTTPResponse, error) {
	var in struct {
		OrderID   uint `json:"order_id"`
		AccountID uint `json:"account_id"`
	}
	if json.Unmarshal(r.Body, &in) != nil {
		return &pluginv1.HTTPResponse{Status: 400}, nil
	}
	out, err := s.reconcile(ctx, in.OrderID)
	if err != nil {
		raw, _ := json.Marshal(map[string]any{"error": err.Error()})
		return &pluginv1.HTTPResponse{Status: 403, Body: raw, ContentType: "application/json"}, nil
	}
	return &pluginv1.HTTPResponse{Status: 200, Body: out.ResultJson, ContentType: "application/json"}, nil
}
func response(value any, err error) (*pluginv1.PageActionResponse, error) {
	if err != nil {
		var call *pluginv1.HostCallError
		if errors.As(err, &call) {
			raw, _ := json.Marshal(map[string]any{"error": call.Code})
			return &pluginv1.PageActionResponse{ResultJson: raw}, nil
		}
		return nil, err
	}
	raw, err := json.Marshal(value)
	return &pluginv1.PageActionResponse{ResultJson: raw}, err
}
func main() { pluginv1.Serve(&server{}) }

// Retry temporary host contention only. Identical fact identity is retained.
func retryHost(ctx context.Context, call func() error) error {
	for attempt := 0; ; attempt++ {
		err := call()
		var hostErr *pluginv1.HostCallError
		temporary := errors.As(err, &hostErr) && (hostErr.Code == "temporary_unavailable" || hostErr.Code == "deadline_exceeded")
		if err != nil && strings.Contains(err.Error(), "HTTP 503") {
			temporary = true
		}
		if err == nil || attempt >= 15 || !temporary {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func listOrders(ctx context.Context, host *pluginv1.HostClient, in pluginv1.CommerceOrdersRequest, out any) error {
	return retryHost(ctx, func() error { return host.ListOrders(ctx, in, out) })
}
func recordPayment(ctx context.Context, host *pluginv1.HostClient, in pluginv1.PaymentFactRequest) (out pluginv1.PaymentFactResult, err error) {
	err = retryHost(ctx, func() error { var e error; out, e = host.RecordPayment(ctx, in); return e })
	return
}

func storageGet(ctx context.Context, storage *pluginv1.HostStorage, key string) (out pluginv1.StoredValue, err error) {
	err = retryHost(ctx, func() error { var e error; out, e = storage.Get(ctx, key); return e })
	return
}
