package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
)

func (s *Services) CompletionAccounting(cipher metering.CredentialDecryptor) metering.CompletionAccounting {
	return metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: s.Identity.db, Cipher: cipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
}

func ApplyFlowBatch(tx *gorm.DB, events []metering.FlowSample, cipher metering.CredentialDecryptor) ([]metering.FlowAccountingResult, error) {
	return (meteringstore.BatchAccounting{Cipher: cipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}).Apply(tx, events)
}
