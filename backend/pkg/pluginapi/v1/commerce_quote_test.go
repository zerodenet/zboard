package pluginv1

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type discountWireServer struct {
	UnimplementedPluginControlServer
	UnimplementedCommerceQuoteProviderServer
}

func TestCommerceQuoteLegacyControlServerDoesNotRegisterQuoteAuthority(t *testing.T) {
	server := grpc.NewServer()
	defer server.Stop()
	if err := (&ControlPlugin{Impl: UnimplementedPluginControlServer{}}).GRPCServer(nil, server); err != nil {
		t.Fatal(err)
	}
	services := server.GetServiceInfo()
	if _, exists := services["zboard.plugin.v1.PluginControl"]; !exists {
		t.Fatal("existing control service missing")
	}
	if _, exists := services["zboard.plugin.v1.CommerceQuoteProvider"]; exists {
		t.Fatal("legacy server acquired quote service")
	}
	if CommerceQuoteProviderCapability == CommercePaymentsWriteCapability {
		t.Fatal("quote and payment grants are not independent")
	}
}

func (discountWireServer) GetInfo(context.Context, *Empty) (*Info, error) {
	return &Info{Id: "example.quote", Protocol: 1, Capabilities: []string{CommerceQuoteProviderCapability}}, nil
}

func (discountWireServer) EvaluateCommerceQuote(_ context.Context, request *CommerceQuoteRequest) (*CommerceQuoteResult, error) {
	in, err := request.DiscountContext()
	if err != nil {
		return nil, err
	}
	terms := `{"discount_minor":2000,"exact":9007199254740993,"html":"<&>"}`
	hash, _ := CommerceDiscountTermsHash(terms)
	return CommerceDiscountResult(CommerceDiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 2000, ExpiresAt: time.Now().Add(time.Minute), Agreement: &CommerceDiscountAgreement{ID: "first", Version: "1", Scope: "initial_only", TermsHash: hash, TermsJSON: terms}})
}

func TestCommerceQuoteIndependentRPCPreservesExistingControlClient(t *testing.T) {
	listener := bufconn.Listen(64 << 10)
	server := grpc.NewServer()
	plugin := &ControlPlugin{Impl: discountWireServer{}}
	if err := plugin.GRPCServer(nil, server); err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "passthrough:///discount-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	raw, err := plugin.GRPCClient(ctx, nil, conn)
	if err != nil {
		t.Fatal(err)
	}
	control, ok := raw.(PluginControlClient)
	if !ok {
		t.Fatal("existing control client assertion broken")
	}
	info, err := control.GetInfo(ctx, &Empty{})
	if err != nil || info.Id != "example.quote" {
		t.Fatal(info, err)
	}
	quote, ok := CommerceQuoteClient(control)
	if !ok {
		t.Fatal("independent quote RPC unavailable")
	}
	in := CommerceDiscountContext{Version: 1, BuyerID: 7, Operation: "new", PlanID: 1, PlanSKUID: 2, BasePriceMinor: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, InputFingerprint: strings.Repeat("a", 64)}
	input, _ := json.Marshal(in)
	result, err := quote.EvaluateCommerceQuote(ctx, &CommerceQuoteRequest{ContextJson: input, Generation: 2, ConfigRevision: 3})
	if err != nil {
		t.Fatal(err)
	}
	var offer CommerceDiscountOffer
	if json.Unmarshal(result.OfferJson, &offer) != nil || offer.DiscountMinor != 2000 || offer.Currency != "CNY" {
		t.Fatal(offer)
	}
	hash, _ := CommerceDiscountTermsHash(offer.Agreement.TermsJSON)
	if hash != offer.Agreement.TermsHash || !strings.Contains(offer.Agreement.TermsJSON, "9007199254740993") {
		t.Fatal("wire changed agreement precision", offer)
	}
}

func TestCommerceQuoteSDKRejectsUnboundedOrUnknownContext(t *testing.T) {
	for _, raw := range []string{`{"version":1,"buyer_id":7,"payment_authority":true}`, `{"version":1,"buyer_id":7} {}`, strings.Repeat(" ", MaxCommerceQuoteBytes+1)} {
		request := &CommerceQuoteRequest{ContextJson: []byte(raw), Generation: 1, ConfigRevision: 1}
		if _, err := request.DiscountContext(); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	request := &CommerceQuoteRequest{ContextJson: []byte(`{"version":1,"buyer_id":7}`), Generation: 1}
	if _, err := request.DiscountContext(); err == nil {
		t.Fatal("unconfigured host accepted")
	}
	if _, err := CommerceDiscountTermsHash("null"); err == nil {
		t.Fatal("null agreement accepted")
	}
}
