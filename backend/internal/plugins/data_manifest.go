package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
)

const MaxStorageBytes = 256 << 10
const MaxStorageValueBytes = 32 << 10

var storageKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type DataManifest struct {
	Version              uint64          `json:"version"`
	MinCompatibleVersion uint64          `json:"min_compatible_version"`
	Migrations           []DataMigration `json:"migrations"`
}
type DataMigration struct {
	Version uint64       `json:"version"`
	Changes []DataChange `json:"changes"`
}
type DataChange struct {
	Target    string          `json:"target"`
	Operation string          `json:"operation"`
	Key       string          `json:"key"`
	To        string          `json:"to,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
}

func (m Manifest) validateData() error {
	storage := slices.Contains(m.Capabilities, StorageCapability)
	if m.Data == nil {
		if storage {
			return errors.New("storage requires a versioned data declaration")
		}
		return nil
	}
	d := m.Data
	if d.Version == 0 || d.Version > 32 || d.MinCompatibleVersion == 0 || d.MinCompatibleVersion > d.Version || len(d.Migrations) != int(d.Version) {
		return errors.New("invalid data version or incomplete migration chain")
	}
	count := 0
	for i, step := range d.Migrations {
		if step.Version != uint64(i+1) {
			return errors.New("migrations must be contiguous and ordered from version 1")
		}
		count += len(step.Changes)
		for _, c := range step.Changes {
			if !storageKeyPattern.MatchString(c.Key) {
				return errors.New("invalid migration key")
			}
			if c.Target != "config" && c.Target != "storage" || c.Target == "storage" && !storage || c.Target == "config" && !slices.Contains(m.Capabilities, ConfigCapability) {
				return errors.New("migration target capability not declared")
			}
			switch c.Operation {
			case "set_default":
				if !json.Valid(c.Value) || len(c.Value) > MaxStorageValueBytes || c.To != "" {
					return errors.New("invalid default value")
				}
			case "rename":
				if !storageKeyPattern.MatchString(c.To) || c.To == c.Key || len(c.Value) > 0 {
					return errors.New("invalid migration rename")
				}
			case "remove":
				if c.To != "" || len(c.Value) > 0 {
					return errors.New("invalid migration removal")
				}
			default:
				return errors.New("unsupported migration operation; SQL and scripts are forbidden")
			}
		}
	}
	if count > 128 {
		return errors.New("too many migration changes")
	}
	return nil
}
func migrationChecksum(step DataMigration) string {
	raw, _ := json.Marshal(step)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func applyDataChange(obj map[string]json.RawMessage, c DataChange) error {
	switch c.Operation {
	case "set_default":
		if _, ok := obj[c.Key]; !ok {
			obj[c.Key] = c.Value
		}
	case "remove":
		delete(obj, c.Key)
	case "rename":
		if value, ok := obj[c.Key]; ok {
			if _, exists := obj[c.To]; exists {
				return errors.New("migration rename destination already exists")
			}
			obj[c.To] = value
			delete(obj, c.Key)
		}
	default:
		return errors.New("invalid data migration operation")
	}
	return nil
}
