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

type redemptionWireServer struct {
	UnimplementedPluginControlServer
}

func (redemptionWireServer) ExecuteCommerceRedemption(_ context.Context, request *CommerceQuoteRequest) (*CommerceQuoteResult, error) {
	in, err := request.DiscountRedemption()
	if err != nil {
		return nil, err
	}
	return CommerceRedemptionResult(DiscountRedemptionResult{Version: 1, Key: in.Binding.Key, BindingHash: in.BindingHash, State: "reserved", Guarantee: DiscountRedemptionGuarantee, Token: "one-exclusive-unit"})
}
func TestCommerceRedemptionOptionalRPCPreservesControlAndLegacyServers(t *testing.T) {
	legacy := grpc.NewServer()
	defer legacy.Stop()
	if err := (&ControlPlugin{Impl: UnimplementedPluginControlServer{}}).GRPCServer(nil, legacy); err != nil {
		t.Fatal(err)
	}
	if _, exists := legacy.GetServiceInfo()["zboard.plugin.v1.CommerceRedemptionProvider"]; exists {
		t.Fatal("legacy server gained redemption authority")
	}
	if DiscountRedemptionCapability == CommercePaymentsWriteCapability || DiscountRedemptionCapability == CommerceQuoteProviderCapability {
		t.Fatal("independent capabilities collapsed")
	}
	listener := bufconn.Listen(64 << 10)
	server := grpc.NewServer()
	plugin := &ControlPlugin{Impl: redemptionWireServer{}}
	if err := plugin.GRPCServer(nil, server); err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "passthrough:///redemption-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	raw, err := plugin.GRPCClient(ctx, nil, conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw.(PluginControlClient); !ok {
		t.Fatal("control transport broken")
	}
	client, ok := raw.(CommerceRedemptionProviderClient)
	if !ok {
		t.Fatal("redemption transport absent")
	}
	binding := DiscountRedemptionBinding{Version: 1, Key: strings.Repeat("a", 64), OrderID: 4, BuyerID: 5, InputFingerprint: strings.Repeat("b", 64), Provider: DiscountProviderIdentity{PluginID: "example.quote", SigningKeyFingerprint: strings.Repeat("c", 64), PackageDigest: strings.Repeat("d", 64), Generation: 1, ConfigRevision: 1}, AgreementID: "finite", AgreementVersion: "1", TermsHash: strings.Repeat("e", 64), BaseMinor: 12000, DiscountMinor: 2000, PayableMinor: 10000, Currency: "CNY"}
	hash, err := DiscountRedemptionHash(binding)
	if err != nil {
		t.Fatal(err)
	}
	in := DiscountRedemptionRequest{Version: 1, Action: "query", Binding: binding, BindingHash: hash}
	encoded, _ := json.Marshal(in)
	// Old accepted binding remains stable when recovery uses a new native runtime.
	result, err := client.ExecuteCommerceRedemption(ctx, &CommerceQuoteRequest{ContextJson: encoded, Generation: 2, ConfigRevision: 3})
	if err != nil {
		t.Fatal(err)
	}
	var out DiscountRedemptionResult
	if json.Unmarshal(result.OfferJson, &out) != nil || ValidateDiscountRedemptionResult(in, out) != nil {
		t.Fatalf("wire binding changed: %+v", out)
	}
	for _, invalid := range [][]byte{append(encoded, []byte(` {}`)...), []byte(`{"version":1,"payment_authority":true}`), []byte(strings.Repeat(" ", MaxDiscountRedemptionBytes+1))} {
		if _, err := (&CommerceQuoteRequest{ContextJson: invalid, Generation: 2, ConfigRevision: 3}).DiscountRedemption(); err == nil {
			t.Fatal("unbounded/unknown redemption envelope accepted")
		}
	}
}
