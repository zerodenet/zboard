package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (m *Manager) checkDataCompatibility(v Installation) error {
	if !v.Data.Compatible || v.Data.MigrationRequired {
		return errors.New("plugin data migration required or data version incompatible; review data management")
	}
	return m.checkMigrationHistory(v)
}
func (m *Manager) checkMigrationHistory(v Installation) error {
	if v.Data.Version == 0 {
		return nil
	}
	if v.Manifest.Data == nil || v.Data.Version > v.Manifest.Data.Version {
		return errors.New("plugin version cannot read current data")
	}
	var rows []model.PluginMigration
	if err := m.db.Where("plugin_id = ? AND epoch = ?", v.ID, v.Data.Epoch).Find(&rows).Error; err != nil {
		return err
	}
	seen := map[uint64]bool{}
	for _, row := range rows {
		if row.Version == 0 || row.Version > v.Data.Version || row.Checksum != migrationChecksum(v.Manifest.Data.Migrations[row.Version-1]) {
			return errors.New("applied migration checksum changed")
		}
		if seen[row.Version] {
			return errors.New("duplicate applied migration")
		}
		seen[row.Version] = true
	}
	if len(seen) != int(v.Data.Version) {
		return errors.New("incomplete migration history")
	}
	return nil
}
func (m *Manager) migrateLocked(ctx context.Context, v Installation, actor string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if v.Enabled || v.State == "uninstalled" || !v.Compatibility.Compatible || !v.Authorization.Reviewed || v.Manifest.Data == nil {
		return ErrPermission
	}
	if err := executionAuthorized(v); err != nil {
		return err
	}
	if err := m.checkMigrationHistory(v); err != nil {
		return err
	}
	if v.Data.Version >= v.Manifest.Data.Version {
		return errors.New("no forward data migration available")
	}
	row, err := m.readData(v.ID)
	if err != nil {
		return err
	}
	obj, err := m.decodeStorage(row)
	if err != nil {
		return err
	}
	config, err := m.config(v)
	if err != nil {
		return err
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(config, &cfg); err != nil {
		return err
	}
	changedConfig := false
	pending := []model.PluginMigration{}
	for _, step := range v.Manifest.Data.Migrations {
		if step.Version <= row.Version {
			continue
		}
		for _, change := range step.Changes {
			target := obj
			cap := StorageCapability
			if change.Target == "config" {
				target = cfg
				cap = ConfigCapability
				changedConfig = true
			}
			if !hasCapability(v, cap) {
				return ErrPermission
			}
			if err := applyDataChange(target, change); err != nil {
				return err
			}
		}
		pending = append(pending, model.PluginMigration{ID: uuid.NewString(), PluginID: v.ID, Epoch: row.Epoch, Version: step.Version, Checksum: migrationChecksum(step), Digest: v.Digest, Actor: actor})
	}
	raw, err := encodeStorage(obj)
	if err != nil {
		return err
	}
	row.Ciphertext, err = m.cipher.Encrypt(string(raw))
	if err != nil {
		return err
	}
	nextConfig, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := validConfig(nextConfig); err != nil {
		return err
	}
	if v.Manifest.Components.Server != nil {
		// Validate only the candidate configuration: the old schema may be unreadable by the new binary.
		proc, err := m.startAuthorizedProcess(ctx, v)
		if err != nil {
			return err
		}
		normalized, err := proc.apply(ctx, nextConfig, v.ConfigRevision+1)
		proc.close()
		if err != nil {
			return err
		}
		if string(normalized) != string(nextConfig) {
			changedConfig = true
		}
		nextConfig = normalized
	}
	encrypted := ""
	if changedConfig {
		encrypted, err = m.cipher.Encrypt(string(nextConfig))
		if err != nil {
			return err
		}
	}
	row.Version = v.Manifest.Data.Version
	row.Revision++
	row.UpdatedAt = time.Now().UTC()
	return m.db.Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return err
		}
		for _, record := range pending {
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"generation": gorm.Expr("generation + 1"), "last_error": ""}
		if changedConfig {
			updates["config_ciphertext"] = encrypted
			updates["config_revision"] = gorm.Expr("config_revision + 1")
		}
		return tx.Model(&model.PluginInstallation{}).Where("id = ?", v.ID).Updates(updates).Error
	})
}
func (m *Manager) purgeDataLocked(v Installation) error {
	row, err := m.readData(v.ID)
	if err != nil {
		return err
	}
	row.Epoch++
	row.Revision++
	row.Version = 0
	row.Ciphertext = ""
	row.UpdatedAt = time.Now().UTC()
	return m.db.Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return err
		}
		return tx.Model(&model.PluginInstallation{}).Where("id = ?", v.ID).Updates(map[string]any{"config_ciphertext": "", "config_revision": gorm.Expr("config_revision + 1"), "generation": gorm.Expr("generation + 1")}).Error
	})
}
