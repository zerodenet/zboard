package server

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/router"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscriptionRouterDynamicPrefixAndLegacyRedirect(t *testing.T) {
	value := ""
	lookupErr := error(nil)
	routing := SubscriptionRouter{LegacyTokenExists: func(_ context.Context, token string) (bool, error) { return token == "secret", nil }, Router: router.NewRouter(), Prefix: func(context.Context) (string, error) { return value, lookupErr }}
	var config rest.RestConf
	config.Name = "subscription-routing"
	config.Port = 19998
	config.Mode = "test"
	ConfigureSafeHTTPLogging(&config)
	srv, err := rest.NewServer(config, rest.WithRouter(routing))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	srv.AddRoute(newRoute(http.MethodGet, platform.LegacySubscriptionPrefix+":token", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sanitizeAccessLogPath(r.URL.Path) + "?" + r.URL.RawQuery))
	}))
	run := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	if w := run("GET", platform.LegacySubscriptionPrefix+"secret"); w.Code != 404 {
		t.Fatal("default enabled", w.Code)
	}
	value = "/private-feed"
	if w := run("GET", platform.LegacySubscriptionPrefix+"random-probe"); w.Code != 404 || w.Header().Get("Location") != "" {
		t.Fatal("random probe exposed private prefix", w.Code, w.Header())
	}

	w := run("GET", platform.LegacySubscriptionPrefix+"secret?template=native&protocol=ss&x=a%2Bb")
	if w.Code != 301 || w.Header().Get("Location") != "/private-feed/secret?template=native&protocol=ss&x=a%2Bb" || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	w = run("GET", "/private-feed/secret?template=native")
	if w.Code != 200 || w.Body.String() != redactedSubscriptionPath+"?template=native" {
		t.Fatal(w.Code, w.Body.String())
	}
	value = "/changed"
	if w := run("GET", "/private-feed/secret"); w.Code == 200 {
		t.Fatal("old configured prefix remained active")
	}
	if w := run("GET", "/changed/secret"); w.Code != 200 {
		t.Fatal("new prefix not immediate", w.Code)
	}
	if w := run("POST", platform.LegacySubscriptionPrefix+"secret"); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := run("GET", platform.LegacySubscriptionPrefix+"a/b"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	lookupErr = errors.New("database unavailable")
	if w := run("GET", "/changed/secret"); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
