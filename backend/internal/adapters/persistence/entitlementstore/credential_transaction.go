package entitlementstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

const (
	credentialTransactionMaxAttempts = 3
	credentialTransactionBaseDelay   = 20 * time.Millisecond
	credentialLockName               = "zboard:protocol-credentials"
	credentialLockTimeoutSeconds     = 10
)

var ErrCredentialLockTimeout = errors.New("protocol credential reconciliation lock timeout")

type credentialLockRow interface {
	Scan(dest ...interface{}) error
}

func RunCredentialTransaction(ctx context.Context, db *gorm.DB, operation func(*gorm.DB) error) error {
	db = db.WithContext(ctx)
	if datastore.IsSQLite(db) {
		// SQLite uses one connection and _txlock=immediate, which serializes writers.
		return retryCredentialTransaction(func() error { return db.Transaction(operation) }, time.Sleep)
	}
	return retryCredentialTransaction(func() error {
		return db.Connection(func(connection *gorm.DB) (err error) {
			acquired, err := acquireCredentialLock(connection)
			if err != nil {
				return err
			}
			if !acquired {
				return ErrCredentialLockTimeout
			}
			defer func() {
				releaseErr := connection.Exec("SELECT RELEASE_LOCK(?)", credentialLockName).Error
				if err == nil {
					err = releaseErr
				}
			}()
			return connection.Transaction(operation)
		})
	}, time.Sleep)
}

func acquireCredentialLock(connection *gorm.DB) (bool, error) {
	row := connection.Raw("SELECT GET_LOCK(?, ?)", credentialLockName, credentialLockTimeoutSeconds).Row()
	return scanCredentialLockResult(row)
}

func scanCredentialLockResult(row credentialLockRow) (bool, error) {
	var result sql.NullInt64
	if err := row.Scan(&result); err != nil {
		return false, err
	}
	return result.Valid && result.Int64 == 1, nil
}

func retryCredentialTransaction(run func() error, sleep func(time.Duration)) error {
	var err error
	for attempt := 1; attempt <= credentialTransactionMaxAttempts; attempt++ {
		err = run()
		if err == nil || !isRetryableCredentialTransactionError(err) || attempt == credentialTransactionMaxAttempts {
			return err
		}
		if sleep != nil {
			sleep(credentialTransactionBaseDelay << (attempt - 1))
		}
	}
	return err
}

func isRetryableCredentialTransactionError(err error) bool {
	if errors.Is(err, ErrCredentialLockTimeout) {
		return true
	}
	var mysqlErr *mysqlDriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	return mysqlErr.Number == 1213 || mysqlErr.Number == 1205
}
