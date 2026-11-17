package pluginv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const CommerceRedemptionMethod = "/zboard.plugin.v1.CommerceRedemptionProvider/ExecuteCommerceRedemption"

// The service deliberately reuses the already generated bounded byte envelopes.
// No PluginControlServer or existing optional quote implementation must change.
type CommerceRedemptionProviderClient interface {
	ExecuteCommerceRedemption(context.Context, *CommerceQuoteRequest, ...grpc.CallOption) (*CommerceQuoteResult, error)
}
type commerceRedemptionProviderClient struct{ cc grpc.ClientConnInterface }

func NewCommerceRedemptionProviderClient(cc grpc.ClientConnInterface) CommerceRedemptionProviderClient {
	return &commerceRedemptionProviderClient{cc: cc}
}
func (c *commerceRedemptionProviderClient) ExecuteCommerceRedemption(ctx context.Context, in *CommerceQuoteRequest, options ...grpc.CallOption) (*CommerceQuoteResult, error) {
	out := new(CommerceQuoteResult)
	if err := c.cc.Invoke(ctx, CommerceRedemptionMethod, in, out, options...); err != nil {
		return nil, err
	}
	return out, nil
}

type CommerceRedemptionProviderServer interface {
	ExecuteCommerceRedemption(context.Context, *CommerceQuoteRequest) (*CommerceQuoteResult, error)
}
type UnimplementedCommerceRedemptionProviderServer struct{}

func (UnimplementedCommerceRedemptionProviderServer) ExecuteCommerceRedemption(context.Context, *CommerceQuoteRequest) (*CommerceQuoteResult, error) {
	return nil, status.Error(codes.Unimplemented, "durable commerce redemption is unsupported")
}
func RegisterCommerceRedemptionProviderServer(registrar grpc.ServiceRegistrar, server CommerceRedemptionProviderServer) {
	registrar.RegisterService(&CommerceRedemptionProviderServiceDesc, server)
}
func commerceRedemptionHandler(server any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(CommerceQuoteRequest)
	if err := decode(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, request any) (any, error) {
		return server.(CommerceRedemptionProviderServer).ExecuteCommerceRedemption(ctx, request.(*CommerceQuoteRequest))
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: server, FullMethod: CommerceRedemptionMethod}, handler)
}

var CommerceRedemptionProviderServiceDesc = grpc.ServiceDesc{
	ServiceName: "zboard.plugin.v1.CommerceRedemptionProvider",
	HandlerType: (*CommerceRedemptionProviderServer)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "ExecuteCommerceRedemption", Handler: commerceRedemptionHandler}},
	Metadata:    "v1/commerce_redemption.proto",
}

func (r *CommerceQuoteRequest) DiscountRedemption() (DiscountRedemptionRequest, error) {
	var out DiscountRedemptionRequest
	if r == nil || r.Generation == 0 || r.ConfigRevision == 0 || len(r.ContextJson) == 0 || len(r.ContextJson) > MaxDiscountRedemptionBytes {
		return out, errors.New("invalid commerce redemption envelope")
	}
	d := json.NewDecoder(bytes.NewReader(r.ContextJson))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || ValidateDiscountRedemptionRequest(out) != nil {
		return out, errors.New("invalid commerce redemption request")
	}
	return out, nil
}
func CommerceRedemptionResult(out DiscountRedemptionResult) (*CommerceQuoteResult, error) {
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > MaxDiscountRedemptionBytes {
		return nil, errors.New("invalid commerce redemption result")
	}
	return &CommerceQuoteResult{OfferJson: raw}, nil
}
