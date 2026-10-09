package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

func (h *handlers) PluginPublicRouteHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, plugins.MaxPublicRouteBodyBytes))
	if err != nil {
		http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
		return
	}
	response, err := h.pluginManager.HandlePublicRoute(r.Context(), pluginv1.NewHTTPRequest(r, body))
	if err != nil {
		switch {
		case errors.Is(err, plugins.ErrRouteRequestTooLarge):
			http.Error(w, "request metadata is too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, plugins.ErrRouteNotFound):
			http.NotFound(w, r)
		case errors.Is(err, plugins.ErrConflict), errors.Is(err, plugins.ErrUnavailable):
			http.Error(w, "plugin route unavailable", http.StatusServiceUnavailable)
		default:
			http.Error(w, "plugin route failed", http.StatusBadGateway)
		}
		return
	}
	w.Header().Set("Content-Type", response.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if response.RetryAfter > 0 && response.RetryAfter <= 3600 {
		w.Header().Set("Retry-After", strconv.FormatInt(response.RetryAfter, 10))
	}
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}
