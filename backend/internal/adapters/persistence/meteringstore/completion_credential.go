package meteringstore

import (
	"crypto/subtle"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Completion and buffered updates remain attributable after a subscription or
// credential has expired. The node event itself is authenticated, so historical
// credentials can be used for ownership resolution without re-enabling access.
func ResolveCompletionCredential(tx *gorm.DB, cipher metering.CredentialDecryptor, nodeID uint, principal string) (model.ProtocolCredential, error) {
	var credential model.ProtocolCredential
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("principal_key = ? AND node_id = ?", principal, nodeID).
		Order("id DESC").
		First(&credential).Error
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		return credential, err
	}
	return matchCredentialSecret(tx, cipher, nodeID, principal)
}

// Current Zero Shadowsocks events use the authenticated password as the
// protocol principal. Match it only in memory, then immediately replace it
// with the stable panel principal; the password is never persisted in traffic
// records or logs. Completed flows may reference a credential that has already
// expired, so ownership lookup intentionally includes historical credentials.
func matchCredentialSecret(tx *gorm.DB, cipher metering.CredentialDecryptor, nodeID uint, provided string) (model.ProtocolCredential, error) {
	var credentials []model.ProtocolCredential
	if err := tx.Where("node_id = ?", nodeID).Order("id DESC").Find(&credentials).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	for _, credential := range credentials {
		secret, err := cipher.Decrypt(credential.Secret)
		if err != nil {
			continue
		}
		if len(secret) == len(provided) && subtle.ConstantTimeCompare([]byte(secret), []byte(provided)) == 1 {
			return credential, nil
		}
	}
	return model.ProtocolCredential{}, gorm.ErrRecordNotFound
}
