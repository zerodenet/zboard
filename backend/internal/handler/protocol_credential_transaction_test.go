package handler

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRetryProtocolCredentialTransactionRetriesDeadlock(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := retryProtocolCredentialTransaction(func() error {
		attempts++
		if attempts == 1 {
			return &mysqlDriver.MySQLError{Number: 1213, Message: "deadlock"}
		}
		return nil
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	})
	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if want := []time.Duration{protocolCredentialTransactionBaseDelay}; !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
}

func TestRetryProtocolCredentialTransactionRetriesSerializationTimeout(t *testing.T) {
	attempts := 0
	err := retryProtocolCredentialTransaction(func() error {
		attempts++
		if attempts == 1 {
			return errProtocolCredentialLockTimeout
		}
		return nil
	}, func(time.Duration) {})
	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRetryProtocolCredentialTransactionStopsAfterLimit(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	lockTimeout := &mysqlDriver.MySQLError{Number: 1205, Message: "lock wait timeout"}
	err := retryProtocolCredentialTransaction(func() error {
		attempts++
		return fmt.Errorf("reconcile credentials: %w", lockTimeout)
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	})
	if !errors.Is(err, lockTimeout) {
		t.Fatalf("error = %v, want wrapped lock timeout", err)
	}
	if attempts != protocolCredentialTransactionMaxAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, protocolCredentialTransactionMaxAttempts)
	}
	want := []time.Duration{
		protocolCredentialTransactionBaseDelay,
		protocolCredentialTransactionBaseDelay * 2,
	}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
}

func TestRetryProtocolCredentialTransactionDoesNotRetryOtherErrors(t *testing.T) {
	attempts := 0
	wantErr := errors.New("validation failed")
	err := retryProtocolCredentialTransaction(func() error {
		attempts++
		return wantErr
	}, func(time.Duration) {
		t.Fatal("unexpected retry delay")
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestOrderedProtocolCredentialSubscriptions(t *testing.T) {
	input := []model.Subscription{{ID: 8}, {ID: 2}, {ID: 5}}
	ordered := orderedProtocolCredentialSubscriptions(input)
	got := []uint{ordered[0].ID, ordered[1].ID, ordered[2].ID}
	want := []uint{2, 5, 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered IDs = %v, want %v", got, want)
	}
	if input[0].ID != 8 {
		t.Fatalf("input slice was mutated: %+v", input)
	}
}
