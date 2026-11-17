package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// SubscriptionRouter resolves runtime configuration before go-zero's static
// route matcher. Rewriting to the internal route also keeps access logs redacted.
type SubscriptionRouter struct {
	httpx.Router
	Prefix            func(context.Context) (string, error)
	LegacyTokenExists func(context.Context, string) (bool, error)
}

func (s SubscriptionRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	legacy := strings.HasPrefix(r.URL.Path, platform.LegacySubscriptionPrefix)
	// All system/API routes bypass the delivery setting lookup.
	if !legacy && reservedSubscriptionPath(r.URL.Path) {
		s.Router.ServeHTTP(w, r)
		return
	}
	value, err := s.Prefix(r.Context())
	prefix, parseErr := platform.SubscriptionPrefix(value)
	if err != nil || parseErr != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "Subscription delivery unavailable", http.StatusServiceUnavailable)
		return
	}
	if legacy {
		if prefix == nil {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimPrefix(r.URL.Path, platform.LegacySubscriptionPrefix)
		if token == "" || strings.Contains(token, "/") {
			http.NotFound(w, r)
			return
		}
		if s.LegacyTokenExists != nil {
			allowed, err := s.LegacyTokenExists(r.Context(), token)
			if err != nil {
				http.Error(w, "Subscription delivery unavailable", http.StatusServiceUnavailable)
				return
			}
			if !allowed {
				http.NotFound(w, r)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		prefix.Path += token
		prefix.RawQuery = r.URL.RawQuery
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Location", prefix.String())
		// No HTML body: it would echo the bearer token into browser previews.
		w.WriteHeader(http.StatusMovedPermanently)
		return
	}
	if prefix != nil && strings.HasPrefix(r.URL.Path, prefix.Path) {
		token := strings.TrimPrefix(r.URL.Path, prefix.Path)
		if token == "" || strings.Contains(token, "/") {
			http.NotFound(w, r)
			return
		}
		copy := r.Clone(r.Context())
		copy.URL.Path = platform.LegacySubscriptionPrefix + token
		copy.URL.RawPath = ""
		copy.RequestURI = copy.URL.RequestURI()
		s.Router.ServeHTTP(w, copy)
		return
	}
	s.Router.ServeHTTP(w, r)
}

func reservedSubscriptionPath(value string) bool {
	_, err := platform.SubscriptionPrefix(value)
	// Prefix validation distinguishes system roots. Ordinary pages with dots,
	// spaces or other invalid prefix forms should retain their normal behavior.
	if err == nil {
		return false
	}
	root := strings.ToLower(strings.Split(strings.TrimPrefix(value, "/"), "/")[0])
	switch root {
	case "api", "admin", "account", "auth", "setup", "assets", "media", "healthz", "readyz", "login", "register", "favicon.ico", "robots.txt", "extensions", "pricing", "terms", "privacy", "refund", "docs":
		return true
	}
	return value == "/"
}
