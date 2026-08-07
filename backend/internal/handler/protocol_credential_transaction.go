package handler

import (
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
)

func (h *handlers) runProtocolCredentialTransaction(operation func(tx *gorm.DB) error) error {
	return retryProtocolCredentialTransaction(func() error {
		return h.db.Transaction(operation)
	}, time.Sleep)
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
