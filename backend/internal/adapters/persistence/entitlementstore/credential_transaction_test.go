package entitlementstore

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type credentialLockRowStub struct {
	result sql.NullInt64
	err    error
}

func (row credentialLockRowStub) Scan(dest ...interface{}) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != 1 {
		return fmt.Errorf("scan destination count = %d", len(dest))
	}
	result, ok := dest[0].(*sql.NullInt64)
	if !ok {
		return fmt.Errorf("scan destination type = %T", dest[0])
	}
	*result = row.result
	return nil
}

func TestScanCredentialLockResult(t *testing.T) {
	for _, test := range []struct {
		result sql.NullInt64
		want   bool
	}{
		{sql.NullInt64{Int64: 1, Valid: true}, true},
		{sql.NullInt64{Int64: 0, Valid: true}, false},
		{sql.NullInt64{}, false},
	} {
		got, err := scanCredentialLockResult(credentialLockRowStub{result: test.result})
		if err != nil || got != test.want {
			t.Fatalf("scan result=%v want=%t err=%v", test.result, test.want, err)
		}
	}
	want := errors.New("scan failed")
	if _, err := scanCredentialLockResult(credentialLockRowStub{err: want}); !errors.Is(err, want) {
		t.Fatalf("scan error = %v", err)
	}
}

func TestRetryCredentialTransaction(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := retryCredentialTransaction(func() error {
		attempts++
		if attempts == 1 {
			return &mysqlDriver.MySQLError{Number: 1213, Message: "deadlock"}
		}
		return nil
	}, func(delay time.Duration) { delays = append(delays, delay) })
	if err != nil || attempts != 2 || !reflect.DeepEqual(delays, []time.Duration{credentialTransactionBaseDelay}) {
		t.Fatalf("retry mismatch: attempts=%d delays=%v err=%v", attempts, delays, err)
	}
	attempts = 0
	err = retryCredentialTransaction(func() error {
		attempts++
		return fmt.Errorf("wait: %w", &mysqlDriver.MySQLError{Number: 1205, Message: "timeout"})
	}, func(time.Duration) {})
	if err == nil || attempts != credentialTransactionMaxAttempts {
		t.Fatalf("retry limit mismatch: attempts=%d err=%v", attempts, err)
	}
}
