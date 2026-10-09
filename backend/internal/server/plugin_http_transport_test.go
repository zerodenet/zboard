package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"github.com/zeromicro/go-zero/rest"
)

func TestPublicPluginRoutePreservesEncodedPayloadThroughNativeChain(t *testing.T) {
	payload := []byte("{ \"event\": \"notification\" }\r\n")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path string
		gunzip     bool
		body, want []byte
	}{
		{"plugin gzip body", "/.well-known/example/v1/notification?nonce=1&nonce=2", true, compressed.Bytes(), compressed.Bytes()},
		{"plugin owns invalid encoding", "/.well-known/example/v1/notification", true, payload, payload},
		{"ordinary API retains decoding", "/api/v1/example", true, compressed.Bytes(), payload},
		{"ordinary API disabled decoding", "/api/v1/example", false, compressed.Bytes(), compressed.Bytes()},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := rest.RestConf{Middlewares: rest.MiddlewaresConf{Gunzip: test.gunzip, MaxBytes: true}, MaxBytes: pluginv1.MaxHTTPBodyBytes}
			middleware := ConfigurePluginHTTPTransport(&config)
			srv, err := rest.NewServer(config)
			if err != nil {
				t.Fatal(err)
			}
			srv.Use(middleware)
			var observed *pluginv1.HTTPRequest
			capture := func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				observed = pluginv1.NewHTTPRequest(r, body)
				w.WriteHeader(http.StatusOK)
			}
			srv.AddRoute(newRoute("POST", "/.well-known/:owner/:version/:route", capture))
			srv.AddRoute(newRoute("POST", "/api/v1/example", capture))
			r := httptest.NewRequest("POST", test.path, bytes.NewReader(test.body))
			r.Header.Set("Content-Encoding", "gzip")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			if w.Code != http.StatusOK || observed == nil || !bytes.Equal(observed.Body, test.want) {
				t.Fatalf("transport transformed input: status=%d request=%v", w.Code, observed)
			}
			if observed.RequestUri != r.RequestURI || observed.RawQuery != r.URL.RawQuery || string(observed.Headers["Content-Encoding"].GetValues()[0]) != "gzip" {
				t.Fatalf("transport transformed metadata: %v", observed)
			}
		})
	}
}
