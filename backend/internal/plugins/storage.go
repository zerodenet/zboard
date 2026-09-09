package plugins

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DataStatus struct {
	Version           uint64 `json:"version"`
	TargetVersion     uint64 `json:"target_version"`
	Epoch             uint64 `json:"epoch"`
	Revision          uint64 `json:"revision"`
	Stored            bool   `json:"stored"`
	Compatible        bool   `json:"compatible"`
	MigrationRequired bool   `json:"migration_required"`
}

func (m *Manager) readData(id string) (model.PluginData, error) {
	row := model.PluginData{PluginID: id}
	err := m.db.Where("plugin_id = ?", id).Limit(1).Find(&row).Error
	return row, err
}
func (m *Manager) loadDataStatus(v *Installation) error {
	row, err := m.readData(v.ID)
	if err != nil {
		return err
	}
	d := DataStatus{Version: row.Version, Epoch: row.Epoch, Revision: row.Revision, Stored: row.Ciphertext != "", Compatible: row.Version == 0}
	if v.Manifest.Data != nil {
		d.TargetVersion = v.Manifest.Data.Version
		d.Compatible = row.Version >= v.Manifest.Data.MinCompatibleVersion && row.Version <= d.TargetVersion
		d.MigrationRequired = row.Version < d.TargetVersion
	}
	v.Data = d
	return nil
}
func (m *Manager) decodeStorage(row model.PluginData) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if row.Ciphertext == "" {
		return obj, nil
	}
	raw, err := m.cipher.Decrypt(row.Ciphertext)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return nil, errors.New("invalid stored plugin data")
	}
	return obj, nil
}
func encodeStorage(obj map[string]json.RawMessage) ([]byte, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	if len(obj) > 128 || len(raw) > MaxStorageBytes {
		return nil, errors.New("plugin storage quota exceeded (128 keys / 256 KiB)")
	}
	for k, v := range obj {
		if !storageKeyPattern.MatchString(k) || len(v) > MaxStorageValueBytes {
			return nil, errors.New("invalid storage key or value exceeds 32 KiB")
		}
	}
	return raw, nil
}

type StorageRequest struct {
	Type     string          `json:"type"`
	Key      string          `json:"key"`
	Revision uint64          `json:"revision"`
	Value    json.RawMessage `json:"value,omitempty"`
}
type StorageResult struct {
	Revision uint64          `json:"revision"`
	Found    bool            `json:"found"`
	Value    json.RawMessage `json:"value,omitempty"`
}

func (m *Manager) SessionStorage(token string, userID uint, admin bool, request StorageRequest) (StorageResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.session(token)
	if err != nil {
		return StorageResult{}, err
	}
	if !admin || s.UserID != userID || s.Surface != "admin" {
		return StorageResult{}, ErrPermission
	}
	return m.storageLocked(s.PluginID, request)
}
func (m *Manager) storageLocked(id string, r StorageRequest) (StorageResult, error) {
	if err := m.guard(m.db); err != nil {
		return StorageResult{}, err
	}
	v, err := m.load(id)
	if err != nil {
		return StorageResult{}, err
	}
	if v.State == "uninstalled" || !hasCapability(v, StorageCapability) || !v.Data.Compatible || v.Data.MigrationRequired {
		return StorageResult{}, ErrPermission
	}
	if err := m.checkDataCompatibility(v); err != nil {
		return StorageResult{}, err
	}
	if !storageKeyPattern.MatchString(r.Key) {
		return StorageResult{}, errors.New("invalid storage key")
	}
	row, err := m.readData(id)
	if err != nil {
		return StorageResult{}, err
	}
	obj, err := m.decodeStorage(row)
	if err != nil {
		return StorageResult{}, err
	}
	if r.Type == "storage.get" {
		value, found := obj[r.Key]
		return StorageResult{Revision: row.Revision, Found: found, Value: value}, nil
	}
	if row.Revision != r.Revision {
		return StorageResult{}, ErrConflict
	}
	switch r.Type {
	case "storage.put":
		if !json.Valid(r.Value) || len(r.Value) > MaxStorageValueBytes {
			return StorageResult{}, errors.New("invalid storage value")
		}
		obj[r.Key] = r.Value
	case "storage.delete":
		delete(obj, r.Key)
	default:
		return StorageResult{}, ErrPermission
	}
	raw, err := encodeStorage(obj)
	if err != nil {
		return StorageResult{}, err
	}
	encrypted, err := m.cipher.Encrypt(string(raw))
	if err != nil {
		return StorageResult{}, err
	}
	row.Ciphertext = encrypted
	row.Revision++
	row.UpdatedAt = time.Now().UTC()
	err = m.db.Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
	})
	return StorageResult{Revision: row.Revision}, err
}
func (m *Manager) Migrations(id string) ([]model.PluginMigration, error) {
	rows := []model.PluginMigration{}
	err := m.db.Where("plugin_id = ?", id).Order("created_at desc").Limit(100).Find(&rows).Error
	return rows, err
}
