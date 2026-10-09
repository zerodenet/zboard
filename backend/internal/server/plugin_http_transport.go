package server

import (
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/handler"
)

// ConfigurePluginHTTPTransport moves automatic gzip decoding out of the native
// chain so public plugin routes receive the original payload for verification.
// Call before constructing the server, then install the returned middleware.
func ConfigurePluginHTTPTransport(config *rest.RestConf) rest.Middleware {
	decodeGzip := config.Middlewares.Gunzip
	config.Middlewares.Gunzip = false
	return func(next http.HandlerFunc) http.HandlerFunc {
		decoded := handler.GunzipHandler(next)
		return func(w http.ResponseWriter, r *http.Request) {
			if !decodeGzip || strings.HasPrefix(r.URL.Path, "/.well-known/") {
				next(w, r)
				return
			}
			decoded.ServeHTTP(w, r)
		}
	}
}
