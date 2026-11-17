package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const MaxPageActionBytes = 64 << 10

var pageActionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)

// SessionPageAction dispatches authenticated business-page and slot actions
// only to the native runtime belonging to that exact contribution session. The runtime gets an
// opaque actor ID, never a ZBoard login token or generic core API.
func (m *Manager) SessionPageAction(ctx context.Context, token string, userID uint, admin bool, action string, payload json.RawMessage) (json.RawMessage, error) {
	return m.sessionAction(ctx, token, userID, admin, 0, action, payload)
}

func (m *Manager) sessionAction(ctx context.Context, token string, userID uint, admin bool, orderID uint, action string, payload json.RawMessage) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !pageActionPattern.MatchString(action) || len(payload) == 0 || len(payload) > MaxPageActionBytes || !json.Valid(payload) {
		return nil, errors.New("invalid plugin action")
	}
	m.mu.Lock()
	session, err := m.session(token)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if session.UserID != userID || (session.Purpose != "business" && session.Purpose != "slot") || (session.Surface == "public") || (session.Surface == "admin" && !admin) {
		m.mu.Unlock()
		return nil, ErrPermission
	}
	process := m.processes[session.PluginID]
	services := m.services
	if err := m.admitPluginCall("page:"+session.PluginID+":"+strconv.FormatUint(uint64(userID), 10), 60); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	var checkoutJSON []byte
	scopedActor := ""
	if orderID != 0 {
		checkout, actor, err := m.checkoutLocked(ctx, token, userID, admin, orderID)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		scopedActor = actor
		checkoutJSON, _ = json.Marshal(checkout)
	}
	if process == nil {
		m.mu.Unlock()
		return nil, ErrUnavailable
	}
	m.mu.Unlock()
	actorID := strconv.FormatUint(uint64(userID), 10)
	if issuer, ok := services.(PluginActorIssuer); ok {
		actorID, admin, err = issuer.PluginActor(ctx, session.PluginID, userID)
		if err != nil {
			if errors.Is(err, ErrPermission) {
				return nil, ErrPermission
			}
			return nil, ErrUnavailable
		}
		if session.Surface == "admin" && !admin {
			return nil, ErrPermission
		}
	} else if issuer, ok := services.(PluginPrincipalIssuer); ok {
		actorID, err = issuer.PluginPrincipal(ctx, session.PluginID, userID)
		if err != nil {
			return nil, ErrUnavailable
		}
	}

	if scopedActor != "" {
		actorID = scopedActor
	}
	response, err := process.api.HandlePageAction(ctx, &pluginv1.PageActionRequest{
		SlotId: session.SlotID, Slot: session.Slot, Purpose: session.Purpose, CheckoutJson: checkoutJSON, PageId: session.PageID, Surface: session.Surface, ActorId: actorID,
		ActorAdmin: admin, Action: action, PayloadJson: payload,
	})
	// Revocation and generation changes take precedence over either success or
	// public plugin rejection, including responses arriving after an upgrade.
	m.mu.Lock()
	_, sessionErr := m.session(token)
	stillActive := m.processes[session.PluginID] == process
	m.mu.Unlock()
	if sessionErr != nil || !stillActive {
		return nil, ErrInvalidSession
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
			return nil, context.DeadlineExceeded
		}
		if failure, ok := pluginv1.ParseActionError(err); ok {
			return nil, failure
		}
		return nil, ErrUnavailable
	}
	if response == nil {
		return nil, ErrUnavailable
	}
	result := json.RawMessage(response.ResultJson)
	if len(result) == 0 || len(result) > MaxPageActionBytes || !json.Valid(result) {
		return nil, fmt.Errorf("%w: plugin returned an invalid page action response", ErrUnavailable)
	}
	return result, nil
}
