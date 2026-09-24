package plugins

import (
	"context"
	"errors"
	"strings"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

const MaxPublicRouteBodyBytes = 8 << 20

var ErrRouteNotFound = errors.New("plugin public route not found")

type PublicRouteResponse struct {
	Status      int
	ContentType string
	Body        []byte
	RetryAfter  int64
}

func (m *Manager) HandlePublicRoute(ctx context.Context, method, routePath, contentType string, body []byte) (PublicRouteResponse, error) {
	if len(body) > MaxPublicRouteBodyBytes {
		return PublicRouteResponse{}, errors.New("plugin public route body is too large")
	}
	m.mu.Lock()
	if m.lost.Load() {
		m.mu.Unlock()
		return PublicRouteResponse{}, ErrUnavailable
	}
	var target *process
	var routeID string
	for id, candidate := range m.processes {
		installation, err := m.load(id)
		if err != nil {
			m.mu.Unlock()
			return PublicRouteResponse{}, err
		}
		if !installation.Enabled || installation.State != "active" || !hasCapability(installation, HTTPRouteCapability) {
			continue
		}
		for _, route := range installation.Manifest.Contributions.HTTPRoutes {
			if route.Method == method && route.Path == routePath {
				if target != nil {
					m.mu.Unlock()
					return PublicRouteResponse{}, ErrConflict
				}
				target, routeID = candidate, route.ID
			}
		}
	}
	m.mu.Unlock()
	if target == nil {
		return PublicRouteResponse{}, ErrRouteNotFound
	}

	callContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := target.api.HandleHTTP(callContext, &pluginv1.HTTPRequest{
		RouteId: routeID, Method: method, Path: routePath, ContentType: contentType, Body: body,
	})
	if err != nil || response == nil {
		return PublicRouteResponse{}, ErrUnavailable
	}
	status := int(response.Status)
	if !allowedPublicRouteStatus(status) || len(response.Body) > MaxPublicRouteBodyBytes || !allowedPublicContentType(response.ContentType) {
		return PublicRouteResponse{}, errors.New("plugin returned an invalid public route response")
	}
	return PublicRouteResponse{
		Status: status, ContentType: response.ContentType, Body: response.Body, RetryAfter: response.RetryAfter,
	}, nil
}

func allowedPublicRouteStatus(status int) bool {
	switch status {
	case 200, 400, 401, 403, 404, 409, 429, 503:
		return true
	default:
		return false
	}
}

func allowedPublicContentType(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	return normalized == "application/json" || normalized == "text/plain"
}
