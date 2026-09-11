package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrConflict = errors.New("plugin changed; refresh and retry")
var ErrUnavailable = errors.New("plugin service unavailable")

type Manager struct {
	db             *gorm.DB
	cipher         *security.CredentialCipher
	options        Options
	fetch          func(context.Context, string, int64) ([]byte, error)
	host           string
	owner          string
	epoch          uint64
	mu             sync.Mutex
	marketMu       sync.Mutex
	processes      map[string]*process
	marketReleases map[string]marketReleaseCache
	marketMetadata map[string]marketMetadataCache
	sessions       map[string]Session
	cancel         context.CancelFunc
	done           chan struct{}
	lost           atomic.Bool
}
type Installation struct {
	Admission Admission  `json:"admission"`
	Data      DataStatus `json:"data"`
	model.PluginInstallation
	Version       string                `json:"version"`
	Digest        string                `json:"digest"`
	Manifest      Manifest              `json:"manifest"`
	Compatibility Compatibility         `json:"compatibility"`
	Versions      []model.PluginVersion `json:"versions"`
}

func NewManager(db *gorm.DB, cipher *security.CredentialCipher, options Options, host string) (*Manager, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	for _, d := range []string{options.Directory, filepath.Join(options.Directory, "versions"), filepath.Join(options.Directory, "sockets")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, err
		}
		info, err := os.Lstat(d)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("unsafe plugin directory")
		}
		if err := os.Chmod(d, 0700); err != nil {
			return nil, err
		}
	}
	m := &Manager{db: db, cipher: cipher, options: options, fetch: fetchRemote, host: host, owner: uuid.NewString(), processes: map[string]*process{}, marketReleases: map[string]marketReleaseCache{}, marketMetadata: map[string]marketMetadataCache{}, sessions: map[string]Session{}, done: make(chan struct{})}
	now := time.Now().UTC()
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.PluginHostLease{ID: 1, Owner: "", ExpiresAt: now.Add(-time.Hour)}).Error; err != nil {
		return nil, err
	}
	res := db.Model(&model.PluginHostLease{}).Where("id = 1 AND expires_at < ?", now).Updates(map[string]any{"owner": m.owner, "epoch": gorm.Expr("epoch + 1"), "expires_at": now.Add(time.Minute)})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		// A standby console must remain available for core business even when
		// another host owns extension execution. It grants no plugin sessions.
		m.lost.Store(true)
		m.cancel = func() {}
		close(m.done)
		return m, nil
	}
	var lease model.PluginHostLease
	if err := db.First(&lease, 1).Error; err != nil {
		return nil, err
	}
	m.epoch = lease.Epoch
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.maintain(ctx)
	m.mu.Lock()
	recoveryErr := m.recover()
	m.mu.Unlock()
	if recoveryErr != nil {
		m.Close()
		return nil, recoveryErr
	}
	return m, nil
}
func (m *Manager) guard(tx *gorm.DB) error {
	if m.lost.Load() {
		return ErrUnavailable
	}
	now := time.Now().UTC()
	var lease model.PluginHostLease
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = 1 AND owner = ? AND epoch = ? AND expires_at > ?", m.owner, m.epoch, now).Take(&lease).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			m.lost.Store(true)
			return ErrUnavailable
		}
		return err
	}

	return nil
}
func (m *Manager) renew() error {
	now := time.Now().UTC()
	r := m.db.Model(&model.PluginHostLease{}).Where("id = 1 AND owner = ? AND epoch = ? AND expires_at > ?", m.owner, m.epoch, now).Update("expires_at", now.Add(time.Minute))
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrUnavailable
	}
	return nil
}
func (m *Manager) maintain(ctx context.Context) {
	defer close(m.done)
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.renew(); err != nil {
				m.lost.Store(true)
				m.mu.Lock()
				for id, p := range m.processes {
					p.close()
					delete(m.processes, id)
				}
				m.sessions = map[string]Session{}
				m.mu.Unlock()
				return
			}
			m.mu.Lock()
			for token, s := range m.sessions {
				if time.Now().After(s.ExpiresAt) {
					delete(m.sessions, token)
				}
			}
			for id, p := range m.processes {
				if p.client.Exited() {
					p.close()
					delete(m.processes, id)
					m.invalidate(id)
					_ = m.updateInstallation(id, map[string]any{"state": "failed", "last_error": "plugin process exited"})
				}
			}
			m.mu.Unlock()
		}
	}
}
func (m *Manager) Close() {
	m.cancel()
	<-m.done
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lost.Store(true)
	for _, p := range m.processes {
		p.close()
	}
	m.sessions = map[string]Session{}
	_ = m.db.Model(&model.PluginHostLease{}).Where("id = 1 AND owner = ? AND epoch = ?", m.owner, m.epoch).Update("expires_at", time.Now().UTC().Add(-time.Second)).Error
}
func (m *Manager) recover() error {
	if err := m.db.Model(&model.PluginOperation{}).Where("state = ?", "running").Updates(map[string]any{"state": "interrupted", "message": "host restarted; committed configuration retained"}).Error; err != nil {
		return err
	}
	var rows []model.PluginInstallation
	if err := m.db.Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		if r.State == "uninstalled" {
			continue
		}
		v, err := m.load(r.ID)
		if err != nil {
			return err
		}
		if !r.Enabled && v.Admission.Accepted && !v.Data.MigrationRequired {
			continue
		}
		pack, err := m.packageFor(v)
		if err == nil {
			err = m.commitCandidate(context.Background(), r, pack, "host-recovery", nil)
		}
		if err != nil {
			m.processes[r.ID].close()
			delete(m.processes, r.ID)
			m.invalidate(r.ID)
			if updateErr := m.updateInstallation(r.ID, map[string]any{"generation": gorm.Expr("generation + 1"), "state": "failed", "last_error": err.Error()}); updateErr != nil {
				return updateErr
			}
		}
	}
	return nil
}
func (m *Manager) load(id string) (Installation, error) {
	var v Installation
	if err := m.db.First(&v.PluginInstallation, "id = ?", id).Error; err != nil {
		return v, err
	}
	var version model.PluginVersion
	if err := m.db.First(&version, "id = ?", v.VersionID).Error; err != nil {
		return v, err
	}
	if err := json.Unmarshal([]byte(version.Manifest), &v.Manifest); err != nil {
		return v, err
	}
	if v.Manifest.Surfaces == nil {
		v.Manifest.Surfaces = []string{}
	}
	if v.Manifest.Contributions.Pages == nil {
		v.Manifest.Contributions.Pages = []Page{}
	}
	v.Version = version.Version
	v.Digest = version.Digest
	v.Compatibility = v.Manifest.Compatibility(m.host)
	if err := m.loadAdmission(&v); err != nil {
		return v, err
	}
	if err := m.loadDataStatus(&v); err != nil {
		return v, err
	}
	err := m.db.Where("plugin_id = ?", id).Order("created_at desc").Limit(30).Find(&v.Versions).Error
	return v, err
}
func (m *Manager) List() ([]Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []model.PluginInstallation
	if err := m.db.Order("created_at desc").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Installation, 0, len(rows))
	for _, r := range rows {
		v, err := m.load(r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (m *Manager) Operations(id string) ([]model.PluginOperation, error) {
	var out []model.PluginOperation
	err := m.db.Where("plugin_id = ?", id).Order("created_at desc").Limit(50).Find(&out).Error
	return out, err
}
func (m *Manager) newOperation(id, action, actor string) (model.PluginOperation, error) {
	op := model.PluginOperation{ID: uuid.NewString(), PluginID: id, Action: action, Actor: actor, State: "running", Message: ""}
	return op, m.db.Create(&op).Error
}
func (m *Manager) finish(op model.PluginOperation, err error) error {
	state, message := "succeeded", ""
	if err != nil {
		state, message = "failed", err.Error()
	}
	save := m.db.Model(&op).Updates(map[string]any{"state": state, "message": message}).Error
	return errors.Join(err, save)
}

// Fence every committed lifecycle transition against a replaced host.
func (m *Manager) updateInstallation(id string, values map[string]any) error {
	return m.db.Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		return tx.Model(&model.PluginInstallation{}).Where("id = ?", id).Updates(values).Error
	})
}
