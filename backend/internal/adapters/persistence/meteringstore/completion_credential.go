package meteringstore

import (
	"crypto/subtle"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Historical ownership is independent of current authorization. SQL contains
// only node-scoped digests; a Shadowsocks password is compared only in memory.
// Production accounting prepares the index before starting its transaction.
func ResolveCompletionCredential(tx *gorm.DB, cipher metering.CredentialDecryptor, nodeID uint, principal string) (model.ProtocolCredential, error) {
	index := credentialstore.PrincipalIndex{DB: tx, Cipher: cipher}
	var candidate model.ProtocolCredential
	var err error
	if _, transactional := tx.Statement.ConnPool.(gorm.TxCommitter); transactional {
		candidate, err = index.LookupPrepared(tx.Statement.Context, nodeID, principal)
	} else {
		candidate, err = index.Lookup(tx.Statement.Context, nodeID, principal)
	}
	if errors.Is(err, credentialstore.ErrPrincipalIndexPending) {
		return model.ProtocolCredential{}, metering.ErrPrincipalAmbiguous
	}
	if err != nil {
		return model.ProtocolCredential{}, err
	}
	// Fulfillment, retirement and reconciliation lock subscription before
	// credential. Re-read both after acquiring these same locks.
	if tx.Dialector.Name() == "sqlite" {
		if err := tx.Model(&model.Subscription{}).Where("id = ?", candidate.SubscriptionID).UpdateColumn("kernel_policy_revision", gorm.Expr("kernel_policy_revision")).Error; err != nil {
			return model.ProtocolCredential{}, err
		}
	}
	var subscription model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&subscription, candidate.SubscriptionID).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	var row model.ProtocolCredential
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&row, candidate.ID).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	if row.SubscriptionID != subscription.ID || !completionPrincipalMatches(cipher, nodeID, principal, row) {
		return model.ProtocolCredential{}, metering.ErrPrincipalAmbiguous
	}
	return row, nil
}

func completionPrincipalMatches(cipher metering.CredentialDecryptor, nodeID uint, principal string, row model.ProtocolCredential) bool {
	if row.PrincipalKey != "" && row.PrincipalKey == principal {
		return true
	}
	if row.RetiredPrincipalHash != "" && row.RetiredPrincipalHash == entitlements.RetiredPrincipalHash(nodeID, principal) {
		return true
	}
	if cipher == nil || row.Secret == "" {
		return false
	}
	plain, err := cipher.Decrypt(row.Secret)
	return err == nil && len(plain) == len(principal) && subtle.ConstantTimeCompare([]byte(plain), []byte(principal)) == 1
}
