package handler

import (
	"database/sql"
	"errors"
	"sort"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	protocolCredentialTransactionMaxAttempts = 3
	protocolCredentialTransactionBaseDelay   = 20 * time.Millisecond
	protocolCredentialLockName               = "zboard:protocol-credentials"
	protocolCredentialLockTimeoutSeconds     = 10
)

var errProtocolCredentialLockTimeout = errors.New("protocol credential reconciliation lock timeout")

func (h *handlers) runProtocolCredentialTransaction(operation func(tx *gorm.DB) error) error {
	return retryProtocolCredentialTransaction(func() error {
		return h.db.Connection(func(connection *gorm.DB) (err error) {
			acquired, err := acquireProtocolCredentialLock(connection)
			if err != nil {
				return err
			}
			if !acquired {
				return errProtocolCredentialLockTimeout
			}
			defer func() {
				releaseErr := connection.Exec("SELECT RELEASE_LOCK(?)", protocolCredentialLockName).Error
				if err == nil {
					err = releaseErr
				}
			}()
			return connection.Transaction(operation)
		})
	}, time.Sleep)
}

func acquireProtocolCredentialLock(connection *gorm.DB) (bool, error) {
	var result sql.NullInt64
	if err := connection.Raw(
		"SELECT GET_LOCK(?, ?)",
		protocolCredentialLockName,
		protocolCredentialLockTimeoutSeconds,
	).Scan(&result).Error; err != nil {
		return false, err
	}
	return result.Valid && result.Int64 == 1, nil
}

func retryProtocolCredentialTransaction(run func() error, sleep func(time.Duration)) error {
	var err error
	for attempt := 1; attempt <= protocolCredentialTransactionMaxAttempts; attempt++ {
		err = run()
		if err == nil || !isRetryableProtocolCredentialTransactionError(err) || attempt == protocolCredentialTransactionMaxAttempts {
			return err
		}
		if sleep != nil {
			sleep(protocolCredentialTransactionBaseDelay << (attempt - 1))
		}
	}
	return err
}

func isRetryableProtocolCredentialTransactionError(err error) bool {
	if errors.Is(err, errProtocolCredentialLockTimeout) {
		return true
	}
	var mysqlErr *mysqlDriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	return mysqlErr.Number == 1213 || mysqlErr.Number == 1205
}

func orderedProtocolCredentialSubscriptions(subscriptions []model.Subscription) []model.Subscription {
	ordered := append([]model.Subscription(nil), subscriptions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}
