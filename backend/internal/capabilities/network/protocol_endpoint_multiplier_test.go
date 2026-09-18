package network

import (
	"context"
	"errors"
	"testing"
)

type protocolEndpointMultiplierRepositoryFunc func(context.Context, uint, uint, int64) (ProtocolEndpoint, error)

func (f protocolEndpointMultiplierRepositoryFunc) UpdateProtocolEndpointMultiplier(ctx context.Context, actor, endpoint uint, multiplier int64) (ProtocolEndpoint, error) {
	return f(ctx, actor, endpoint, multiplier)
}

func TestProtocolEndpointMultiplierValidatesAndDelegates(t *testing.T) {
	service := ProtocolEndpointMultiplier{Repository: protocolEndpointMultiplierRepositoryFunc(func(_ context.Context, actor, endpoint uint, multiplier int64) (ProtocolEndpoint, error) {
		if actor != 3 || endpoint != 7 || multiplier != 2500 {
			t.Fatalf("unexpected request: actor=%d endpoint=%d multiplier=%d", actor, endpoint, multiplier)
		}
		return ProtocolEndpoint{ID: endpoint, MultiplierMilli: multiplier}, nil
	})}
	got, err := service.Update(context.Background(), 3, 7, 2500)
	if err != nil || got.ID != 7 || got.MultiplierMilli != 2500 {
		t.Fatalf("result=%+v error=%v", got, err)
	}
	for _, multiplier := range []int64{0, 100001} {
		_, err := service.Update(context.Background(), 3, 7, multiplier)
		var validation *ProtocolEndpointMultiplierValidation
		if !errors.As(err, &validation) || validation.Fields["multiplier_milli"] == "" {
			t.Fatalf("multiplier=%d error=%v", multiplier, err)
		}
	}
}
