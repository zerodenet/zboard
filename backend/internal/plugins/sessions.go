package plugins

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path"
	"time"
)

type Session struct {
	Token       string    `json:"token"`
	BridgeToken string    `json:"bridge_token"`
	PluginID    string    `json:"plugin_id"`
	PageID      string    `json:"page_id"`
	Surface     string    `json:"surface"`
	Purpose     string    `json:"purpose"`
	Entrypoint  string    `json:"entrypoint"`
	URL         string    `json:"url"`
	Generation  uint64    `json:"generation"`
	ExpiresAt   time.Time `json:"expires_at"`
	UserID      uint      `json:"-"`
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (m *Manager) invalidate(id string) {
	for token, s := range m.sessions {
		if s.PluginID == id {
			delete(m.sessions, token)
		}
	}
}
func (m *Manager) CreateSession(id, pageID, surface string, userID uint, admin, configuration bool) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db); err != nil {
		return Session{}, err
	}
	if len(m.sessions) >= 2048 {
		return Session{}, errors.New("too many active plugin sessions")
	}
	v, err := m.load(id)
	if err != nil {
		return Session{}, err
	}
	if !v.Compatibility.Compatible || v.State == "uninstalled" || !hasCapability(v, PageCapability) || (configuration && !hasCapability(v, ConfigCapability)) {
		return Session{}, ErrUnavailable
	}
	if surface == "admin" && !admin || surface == "account" && userID == 0 {
		return Session{}, errors.New("plugin surface is not authorized")
	}
	purpose := "business"
	if configuration {
		purpose = "configuration"
		if !admin || surface != "admin" {
			return Session{}, errors.New("configuration requires administrator")
		}
	} else if !v.Enabled || v.State != "active" || !hasCapability(v, PageCapability) {
		return Session{}, ErrUnavailable
	}
	found := false
	for _, p := range v.Manifest.Contributions.Pages {
		pp := p.Purpose
		if pp == "" {
			pp = "business"
		}
		if p.ID == pageID && p.Surface == surface && pp == purpose {
			found = true
		}
	}
	if !found {
		return Session{}, errors.New("plugin page not declared")
	}
	s := Session{Token: randomToken(), BridgeToken: randomToken(), PluginID: id, PageID: pageID, Surface: surface, Purpose: purpose, Entrypoint: v.Manifest.Components.UI[surface], Generation: v.Generation, ExpiresAt: time.Now().Add(10 * time.Minute), UserID: userID}
	s.URL = "/api/v1/plugin-assets/" + s.Token + "/" + s.Entrypoint + "#bridge_token=" + s.BridgeToken
	m.sessions[s.Token] = s
	return s, nil
}
func (m *Manager) CheckSession(token string, userID uint, admin bool) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.session(token)
	if err != nil {
		return s, err
	}
	if s.UserID != userID || (s.Surface == "admin" && !admin) {
		return Session{}, errors.New("plugin session identity mismatch")
	}
	return s, nil
}
func (m *Manager) session(token string) (Session, error) {
	s, ok := m.sessions[token]
	if !ok || time.Now().After(s.ExpiresAt) || m.lost.Load() {
		return Session{}, ErrUnavailable
	}
	v, err := m.load(s.PluginID)
	if err != nil || s.Generation != v.Generation || !hasCapability(v, PageCapability) || (s.Purpose == "configuration" && !hasCapability(v, ConfigCapability)) || v.State == "uninstalled" || (s.Purpose != "configuration" && (!v.Enabled || v.State != "active")) {
		return Session{}, ErrUnavailable
	}
	return s, nil
}
func (m *Manager) Asset(token, filename string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.session(token)
	if err != nil {
		return nil, err
	}
	if !SafePath(filename) || path.Ext(filename) == ".map" || len(filename) < 3 || filename[:3] != "ui/" {
		return nil, errors.New("invalid asset")
	}
	v, err := m.load(s.PluginID)
	if err != nil {
		return nil, err
	}
	p, err := m.packageFor(v)
	if err != nil {
		return nil, err
	}
	data, ok := p.Files[filename]
	if !ok {
		return nil, errors.New("asset not declared")
	}
	return data, nil
}

type CatalogPage struct {
	PluginID   string `json:"plugin_id"`
	Page       Page   `json:"page"`
	Generation uint64 `json:"generation"`
}

func (m *Manager) Pages(surface string, userID uint, admin bool) ([]CatalogPage, error) {
	if surface != "public" && surface != "account" && surface != "admin" {
		return nil, errors.New("unknown surface")
	}
	if surface == "admin" && !admin || surface == "account" && userID == 0 {
		return nil, errors.New("surface not authorized")
	}
	rows, err := m.List()
	if err != nil {
		return nil, err
	}
	out := []CatalogPage{}
	if m.lost.Load() {
		return out, nil
	}
	for _, v := range rows {
		if !v.Enabled || v.State != "active" || !hasCapability(v, PageCapability) {
			continue
		}
		for _, p := range v.Manifest.Contributions.Pages {
			if p.Surface == surface && p.Purpose != "configuration" {
				out = append(out, CatalogPage{PluginID: v.ID, Page: p, Generation: v.Generation})
			}
		}
	}
	return out, nil
}

func (m *Manager) RevokeSession(token string, userID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[token]; ok && s.UserID == userID {
		delete(m.sessions, token)
	}
}

// Called while holding the lifecycle mutex, immediately before a mutation.
func (m *Manager) configurationSession(token string, userID uint, admin bool) (Session, error) {
	s, err := m.session(token)
	if err != nil {
		return Session{}, err
	}
	if !admin || s.UserID != userID || s.Purpose != "configuration" || s.Surface != "admin" {
		return Session{}, ErrUnavailable
	}
	return s, nil
}
