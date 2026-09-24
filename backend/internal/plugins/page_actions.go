package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

const MaxPageActionBytes = 64 << 10

var pageActionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)

// SessionPageAction dispatches an authenticated business-page action only to
// the native runtime belonging to that exact page session. The runtime gets an
// opaque actor ID, never a ZBoard login token or generic core API.
func (m *Manager) SessionPageAction(ctx context.Context, token string, userID uint, admin bool, action string, payload json.RawMessage) (json.RawMessage, error) {
	if !pageActionPattern.MatchString(action) || len(payload) == 0 || len(payload) > MaxPageActionBytes || !json.Valid(payload) {
		return nil, errors.New("invalid plugin page action")
	}
	m.mu.Lock()
	session, err := m.session(token)
	if err != nil || session.UserID != userID || session.Purpose != "business" || (session.Surface == "admin" && !admin) {
		m.mu.Unlock()
		return nil, ErrPermission
	}
	process := m.processes[session.PluginID]
	services := m.services
	if process == nil {
		m.mu.Unlock()
		return nil, ErrUnavailable
	}
	m.mu.Unlock()
	if services == nil {
		return nil, ErrUnavailable
	}
	principalID, err := services.PluginPrincipal(ctx, session.PluginID, userID)
	if err != nil {
		return nil, ErrPermission
	}

	callContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, err := process.api.HandlePageAction(callContext, &pluginv1.PageActionRequest{
		PageId: session.PageID, Surface: session.Surface, ActorId: principalID,
		ActorAdmin: admin, Action: action, PayloadJson: payload,
	})
	if err != nil || response == nil {
		return nil, ErrUnavailable
	}
	result := json.RawMessage(response.ResultJson)
	if len(result) == 0 || len(result) > MaxPageActionBytes || !json.Valid(result) {
		return nil, errors.New("plugin returned an invalid page action response")
	}
	return result, nil
}
