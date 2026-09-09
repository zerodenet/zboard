package plugins

import (
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
		return errors.New("plugin lifecycle data preparation is incomplete or incompatible")
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

// migrationPlan holds candidate data only. No persistent state changes before
// the enclosing installation transaction commits the package and this plan.
type migrationPlan struct {
	row           *model.PluginData
	records       []model.PluginMigration
	config        []byte
	configChanged bool
}

func (m *Manager) planMigration(v Installation, actor string) (migrationPlan, error) {
	plan := migrationPlan{}
	if err := m.checkMigrationHistory(v); err != nil {
		return plan, err
	}
	config, err := m.config(v)
	if err != nil {
		return plan, err
	}
	plan.config = config
	if v.Manifest.Data == nil || v.Data.Version == v.Manifest.Data.Version {
		return plan, nil
	}
	row, err := m.readData(v.ID)
	if err != nil {
		return plan, err
	}
	obj, err := m.decodeStorage(row)
	if err != nil {
		return plan, err
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(config, &cfg); err != nil {
		return plan, err
	}
	for _, step := range v.Manifest.Data.Migrations {
		if step.Version <= row.Version {
			continue
		}
		for _, change := range step.Changes {
			target, capability := obj, StorageCapability
			if change.Target == "config" {
				target, capability = cfg, ConfigCapability
				plan.configChanged = true
			}
			if !hasCapability(v, capability) {
				return plan, ErrPermission
			}
			if err := applyDataChange(target, change); err != nil {
				return plan, err
			}
		}
		plan.records = append(plan.records, model.PluginMigration{ID: uuid.NewString(), PluginID: v.ID, Epoch: row.Epoch, Version: step.Version, Checksum: migrationChecksum(step), Digest: v.Digest, Actor: actor})
	}
	raw, err := encodeStorage(obj)
	if err != nil {
		return plan, err
	}
	row.Ciphertext, err = m.cipher.Encrypt(string(raw))
	if err != nil {
		return plan, err
	}
	plan.config, err = json.Marshal(cfg)
	if err != nil {
		return plan, err
	}
	if err := validConfig(plan.config); err != nil {
		return plan, err
	}
	row.Version = v.Manifest.Data.Version
	row.Revision++
	row.UpdatedAt = time.Now().UTC()
	plan.row = &row
	return plan, nil
}
func (plan migrationPlan) commit(tx *gorm.DB) error {
	if plan.row != nil {
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(plan.row).Error; err != nil {
			return err
		}
	}
	for _, record := range plan.records {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
	}
	return nil
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
