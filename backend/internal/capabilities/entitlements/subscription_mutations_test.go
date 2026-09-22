package entitlements

import (
	"context"
	"errors"
	"testing"
)

func TestSubscriptionMutationRejectsUnboundedOrAmbiguousCommands(t *testing.T) {
	service := SubscriptionMutations{}
	valid := SubscriptionMutationInput{SubscriptionID: 1, Kind: SubscriptionExtend, Days: 1, Reason: "support extension", IdempotencyKey: "one"}
	for _, mutate := range []func(*SubscriptionMutationInput){
		func(in *SubscriptionMutationInput) { in.SubscriptionID = 0 },
		func(in *SubscriptionMutationInput) { in.Days = 0 },
		func(in *SubscriptionMutationInput) { in.Days = 3651 },
		func(in *SubscriptionMutationInput) { in.Kind = "status.active" },
		func(in *SubscriptionMutationInput) { in.IdempotencyKey = " " },
		func(in *SubscriptionMutationInput) { in.Reason = "x" },
	} {
		in := valid
		mutate(&in)
		if _, err := service.Apply(context.Background(), 1, in); !errors.Is(err, ErrSubscriptionMutationInvalid) {
			t.Fatalf("invalid command %+v: %v", in, err)
		}
	}
	if _, err := service.Apply(context.Background(), 0, valid); !errors.Is(err, ErrAccessPermission) {
		t.Fatalf("missing actor: %v", err)
	}
	if _, err := service.Apply(context.Background(), 1, valid); !errors.Is(err, ErrSubscriptionMutationUnavailable) {
		t.Fatalf("missing store: %v", err)
	}
}
