package network

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestObservationRetryValidatesInspectedIdentity(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, attempt := range []string{"1", "9007199254740993", "18446744073709551614"} {
		in, n, err := (ObservationIntentRetryRequest{ExpectedAttempts: attempt, Reason: "  administrator inspected original asset  "}).Validate(id)
		if err != nil || n == 0 || in.ExpectedAttempts != attempt || in.Reason != "administrator inspected original asset" {
			t.Fatalf("valid candidate %+v %d %v", in, n, err)
		}
	}
	for _, attempt := range []string{"", "0", "01", "+1", " 1", "1.0", "18446744073709551615", "18446744073709551616"} {
		if _, _, err := (ObservationIntentRetryRequest{ExpectedAttempts: attempt, Reason: "valid reason"}).Validate(id); !errors.Is(err, ErrObservationIntentRetryInvalid) {
			t.Fatalf("invalid generation %q %v", attempt, err)
		}
	}
	for _, reason := range []string{" ", "ab", strings.Repeat("中", 134), string([]byte{'a', 'b', 0xff})} {
		if _, _, err := (ObservationIntentRetryRequest{ExpectedAttempts: "1", Reason: reason}).Validate(id); !errors.Is(err, ErrObservationIntentRetryInvalid) {
			t.Fatalf("invalid reason %v", err)
		}
	}
	in := ObservationIntentRetryRequest{ExpectedAttempts: "1", Reason: "valid reason"}
	if _, err := (ObservationIntentOperations{}).Retry(context.Background(), 0, id, in); !errors.Is(err, ErrNodeAdministrationPermission) {
		t.Fatal(err)
	}
	if _, err := (ObservationIntentOperations{}).Retry(context.Background(), 1, id, in); !errors.Is(err, ErrNodeAdministrationUnavailable) {
		t.Fatal(err)
	}
	if _, _, err := in.Validate(strings.ToUpper(id)); !errors.Is(err, ErrObservationIntentRetryInvalid) {
		t.Fatal("uppercase source accepted", err)
	}
}
