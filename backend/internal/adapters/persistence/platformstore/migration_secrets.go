package platformstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// NormalizeMigrationSecrets upgrades legacy SQLite task payloads before copying
// into MySQL JSON columns. Ciphertext is preserved; no secret is decrypted.
func NormalizeMigrationSecrets(db *gorm.DB) error {
	var cursor uint
	for {
		var rows []model.Task
		if err := db.Select("id", "content").Where("type = ? AND id > ?", "database_migration", cursor).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			for _, row := range rows {
				cipher, err := platform.DecodeMigrationSecret(row.Content)
				if err != nil {
					return err
				}
				encoded := platform.EncodeMigrationSecret(cipher)
				if encoded == row.Content {
					continue
				}
				if err := tx.Model(&model.Task{}).Where("id = ? AND content = ?", row.ID, row.Content).Update("content", encoded).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		cursor = rows[len(rows)-1].ID
	}
}
