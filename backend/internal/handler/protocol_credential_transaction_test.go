package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type protocolCredentialLockRowStub struct {
	result sql.NullInt64
	err    error
}

func (row protocolCredentialLockRowStub) Scan(dest ...interface{}) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != 1 {
		return fmt.Errorf("scan destination count = %d, want 1", len(dest))
	}
	result, ok := dest[0].(*sql.NullInt64)
	if !ok {
		return fmt.Errorf("scan destination type = %T, want *sql.NullInt64", dest[0])
	}
	*result = row.result
	return nil
}

func TestScanProtocolCredentialLockResult(t *testing.T) {
	tests := []struct {
		name   string
		result sql.NullInt64
		want   bool
	}{
		{name: "acquired", result: sql.NullInt64{Int64: 1, Valid: true}, want: true},
		{name: "not acquired", result: sql.NullInt64{Int64: 0, Valid: true}, want: false},
		{name: "null result", result: sql.NullInt64{}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := scanProtocolCredentialLockResult(protocolCredentialLockRowStub{result: test.result})
			if err != nil {
				t.Fatalf("scanProtocolCredentialLockResult() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("scanProtocolCredentialLockResult() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestScanProtocolCredentialLockResultReturnsScanError(t *testing.T) {
	wantErr := errors.New("scan failed")
	if _, err := scanProtocolCredentialLockResult(protocolCredentialLockRowStub{err: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("scanProtocolCredentialLockResult() error = %v, want %v", err, wantErr)
	}
}

func TestAcquireProtocolCredentialLockUsesScalarRowScan(t *testing.T) {
	source, err := os.ReadFile("protocol_credential_transaction.go")
	if err != nil {
		t.Fatalf("read protocol credential transaction source: %v", err)
	}
	text := string(source)
	if !strings.Contains(text, ").Row()") || !strings.Contains(text, "return scanProtocolCredentialLockResult(row)") {
		t.Fatal("GET_LOCK result is not passed through database/sql row scanning")
	}
	if strings.Contains(text, ").Scan(&result).Error") {
		t.Fatal("GET_LOCK result regressed to GORM model scanning")
	}
}

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

func TestProtocolCredentialsCurrentForEndpoints(t *testing.T) {
	expiresAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	subscription := model.Subscription{ID: 9, UserID: 7, EndAt: expiresAt}
	endpoint := model.ProtocolEndpoint{
		ID:         11,
		NodeID:     5,
		Protocol:   "vless",
		Port:       443,
		PublicPort: 8443,
	}
	current := model.ProtocolCredential{
		SubscriptionID:     subscription.ID,
		UserID:             subscription.UserID,
		ProtocolEndpointID: endpoint.ID,
		NodeID:             endpoint.NodeID,
		ListenPort:         endpoint.Port,
		PublicPort:         endpoint.PublicPort,
		Status:             protocolCredentialStatusActive,
		ExpiresAt:          subscription.EndAt,
	}

	tests := []struct {
		name        string
		endpoints   []model.ProtocolEndpoint
		credentials []model.ProtocolCredential
		want        bool
	}{
		{name: "current", endpoints: []model.ProtocolEndpoint{endpoint}, credentials: []model.ProtocolCredential{current}, want: true},
		{name: "missing", endpoints: []model.ProtocolEndpoint{endpoint}, want: false},
		{name: "stale node", endpoints: []model.ProtocolEndpoint{endpoint}, credentials: []model.ProtocolCredential{func() model.ProtocolCredential {
			stale := current
			stale.NodeID++
			return stale
		}()}, want: false},
		{name: "revoked", endpoints: []model.ProtocolEndpoint{endpoint}, credentials: []model.ProtocolCredential{func() model.ProtocolCredential {
			revoked := current
			revokedAt := expiresAt.Add(-time.Hour)
			revoked.RevokedAt = &revokedAt
			return revoked
		}()}, want: false},
		{name: "unmanaged endpoint", endpoints: []model.ProtocolEndpoint{{ID: 12, Protocol: "socks5"}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := protocolCredentialsCurrentForEndpoints(subscription, test.endpoints, test.credentials, false); got != test.want {
				t.Fatalf("protocolCredentialsCurrentForEndpoints() = %t, want %t", got, test.want)
			}
		})
	}
}
