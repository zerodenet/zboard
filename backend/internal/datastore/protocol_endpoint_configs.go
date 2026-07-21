package datastore

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
)

// MigrateProtocolEndpointConfigs encrypts legacy plaintext server-side
// protocol configuration. Client configuration remains public subscription data.
func MigrateProtocolEndpointConfigs(db *gorm.DB, credentialCipher *security.CredentialCipher) (int, error) {
	var endpoints []model.ProtocolEndpoint
	if err := db.Where("server_config IS NOT NULL AND server_config <> ''").Find(&endpoints).Error; err != nil {
		return 0, fmt.Errorf("load protocol endpoint configs: %w", err)
	}
	migrated := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, endpoint := range endpoints {
			if security.IsEncryptedCredential(endpoint.ServerConfig) {
				if _, err := credentialCipher.Decrypt(endpoint.ServerConfig); err != nil {
					return fmt.Errorf("validate protocol endpoint %d config: %w", endpoint.ID, err)
				}
				continue
			}
			encrypted, err := credentialCipher.Encrypt(endpoint.ServerConfig)
			if err != nil {
				return fmt.Errorf("encrypt protocol endpoint %d config: %w", endpoint.ID, err)
			}
			if err := tx.Model(&model.ProtocolEndpoint{}).Where("id = ?", endpoint.ID).Update("server_config", encrypted).Error; err != nil {
				return err
			}
			migrated++
		}
		return nil
	})
	return migrated, err
}
