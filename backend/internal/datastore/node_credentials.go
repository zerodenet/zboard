package datastore

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
)

func MigrateNodeCredentials(db *gorm.DB, credentialCipher *security.CredentialCipher) (int, error) {
	var nodes []model.Node
	if err := db.Where("ssh_pwd IS NOT NULL AND ssh_pwd <> ''").Find(&nodes).Error; err != nil {
		return 0, fmt.Errorf("load node credentials: %w", err)
	}

	migrated := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, node := range nodes {
			secured, changed, err := secureNodeCredential(node.SSHPwd, credentialCipher)
			if err != nil {
				return fmt.Errorf("secure node %d credential: %w", node.ID, err)
			}
			if !changed {
				continue
			}
			if err := tx.Model(&model.Node{}).Where("id = ?", node.ID).Update("ssh_pwd", secured).Error; err != nil {
				return fmt.Errorf("update node %d credential: %w", node.ID, err)
			}
			migrated++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return migrated, nil
}

func secureNodeCredential(value string, credentialCipher *security.CredentialCipher) (string, bool, error) {
	if security.IsEncryptedCredential(value) {
		if _, err := credentialCipher.Decrypt(value); err != nil {
			return "", false, err
		}
		return value, false, nil
	}
	encrypted, err := credentialCipher.Encrypt(value)
	if err != nil {
		return "", false, err
	}
	return encrypted, true, nil
}
