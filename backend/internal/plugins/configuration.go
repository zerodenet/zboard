package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"slices"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"gorm.io/gorm"
)

func validConfig(raw []byte) error {
	if len(raw) > MaxConfigBytes {
		return errors.New("configuration exceeds 64 KiB")
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return errors.New("configuration must be a JSON object")
	}
	return nil
}
func (m *Manager) config(v Installation) ([]byte, error) {
	if v.ConfigCiphertext == "" {
		return []byte("{}"), nil
	}
	s, err := m.cipher.Decrypt(v.ConfigCiphertext)
	return []byte(s), err
}

type ConfigView struct {
	Config     json.RawMessage `json:"config,omitempty"`
	Revision   uint64          `json:"revision"`
	Configured bool            `json:"configured"`
}

func (m *Manager) Config(id string) (ConfigView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, err := m.load(id)
	view := ConfigView{Revision: v.ConfigRevision, Configured: v.ConfigCiphertext != ""}
	if err != nil || v.Manifest.Components.Server == nil || !slices.Contains(v.Manifest.Capabilities, "zboard.config.v1") || v.ConfigCiphertext == "" {
		return view, err
	}
	if err := m.guard(m.db); err != nil {
		return view, err
	}
	raw, err := m.config(v)
	if err != nil {
		return view, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	proc := m.processes[id]
	if proc == nil {
		proc, err = m.prepare(ctx, v)
		if err != nil {
			return view, err
		}
		defer proc.close()
	}
	projected, err := proc.api.DescribeConfig(ctx, &pluginv1.ConfigRequest{ConfigJson: raw, Revision: v.ConfigRevision})
	if status.Code(err) == codes.Unimplemented {
		return view, nil
	}
	if err != nil {
		return view, errors.New("configuration view unavailable")
	}
	if projected == nil || validConfig(projected.NormalizedJson) != nil {
		return view, errors.New("invalid configuration view")
	}
	view.Config = projected.NormalizedJson
	return view, nil
}
func (m *Manager) SaveConfig(ctx context.Context, id, actor string, revision uint64, raw []byte) (ConfigView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveConfigLocked(ctx, id, actor, revision, raw)
}
func (m *Manager) SaveSessionConfig(ctx context.Context, token string, userID uint, admin bool, actor string, revision uint64, raw []byte) (ConfigView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.configurationSession(token, userID, admin)
	if err != nil {
		return ConfigView{}, err
	}
	return m.saveConfigLocked(ctx, s.PluginID, actor, revision, raw)
}
func (m *Manager) saveConfigLocked(ctx context.Context, id, actor string, revision uint64, raw []byte) (ConfigView, error) {
	if err := validConfig(raw); err != nil {
		return ConfigView{}, err
	}
	if err := m.guard(m.db); err != nil {
		return ConfigView{}, err
	}
	v, err := m.load(id)
	if err != nil {
		return ConfigView{}, err
	}
	if v.ConfigRevision != revision {
		return ConfigView{}, ErrConflict
	}
	if v.State == "uninstalled" || !v.Compatibility.Compatible || !slices.Contains(v.Manifest.Capabilities, "zboard.config.v1") {
		return ConfigView{}, errors.New("plugin is not configurable")
	}
	op, err := m.newOperation(id, "configure", actor)
	if err != nil {
		return ConfigView{}, err
	}
	old, err := m.config(v)
	if err != nil {
		return ConfigView{}, m.finish(op, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	proc := m.processes[id]
	temporary := false
	if v.Manifest.Components.Server != nil && proc == nil {
		proc, err = m.prepare(ctx, v)
		temporary = true
		if err != nil {
			return ConfigView{}, m.finish(op, err)
		}
		defer proc.close()
	}
	next := raw
	if proc != nil {
		next, err = proc.apply(ctx, raw, revision+1, old)
	}
	if err == nil {
		var encrypted string
		encrypted, err = m.cipher.Encrypt(string(next))
		if err == nil {
			err = m.db.Transaction(func(tx *gorm.DB) error {
				if err := m.guard(tx); err != nil {
					return err
				}
				res := tx.Model(&model.PluginInstallation{}).Where("id = ? AND config_revision = ?", id, revision).Updates(map[string]any{"config_ciphertext": encrypted, "config_revision": revision + 1})
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected != 1 {
					return ErrConflict
				}
				return nil
			})
		}
	}
	if err != nil && proc != nil && !temporary {
		restore, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, restoreErr := proc.apply(restore, old, revision)
		cancel()
		if restoreErr != nil {
			proc.close()
			delete(m.processes, id)
			m.invalidate(id)
			_ = m.updateInstallation(id, map[string]any{"state": "failed", "last_error": "configuration recovery failed"})
		}
	}
	if err = m.finish(op, err); err != nil {
		return ConfigView{}, err
	}
	return ConfigView{Revision: revision + 1, Configured: true}, nil
}
func (m *Manager) TestConfig(ctx context.Context, id, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.testConfigLocked(ctx, id, actor)
}
func (m *Manager) TestSessionConfig(ctx context.Context, token string, userID uint, admin bool, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.configurationSession(token, userID, admin)
	if err != nil {
		return err
	}
	return m.testConfigLocked(ctx, s.PluginID, actor)
}
func (m *Manager) testConfigLocked(ctx context.Context, id, actor string) error {
	if err := m.guard(m.db); err != nil {
		return err
	}
	v, err := m.load(id)
	if err != nil {
		return err
	}
	if v.State == "uninstalled" || !v.Compatibility.Compatible || v.Manifest.Components.Server == nil {
		return errors.New("this UI plugin has no server diagnostic")
	}
	op, err := m.newOperation(id, "test", actor)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	proc := m.processes[id]
	if proc == nil {
		proc, err = m.prepare(ctx, v)
		if err != nil {
			return m.finish(op, err)
		}
		defer proc.close()
	}
	raw, err := m.config(v)
	if err == nil {
		var res *pluginv1.HealthResult
		res, err = proc.api.TestConfig(ctx, &pluginv1.ConfigRequest{ConfigJson: raw, Revision: v.ConfigRevision})
		if err != nil || !res.Healthy {
			err = errors.New("plugin diagnostic failed; review plugin configuration")
		}
	}
	return m.finish(op, err)
}
