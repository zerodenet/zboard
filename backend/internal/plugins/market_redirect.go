package plugins

import (
	"errors"
	"net/http"
)

func validateMarketRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many market redirects")
	}
	// GitHub release assets redirect to expiring, query-signed HTTPS URLs.
	// The transport still resolves and rejects every private destination at dial
	// time, including DNS rebinding; no proxy or credentials are forwarded.
	destination := *req.URL
	destination.RawQuery, destination.ForceQuery = "", false
	if _, err := safeRemoteURL(destination.String()); err != nil {
		return err
	}
	req.Header.Del("Authorization")
	req.Header.Del("Cookie")
	return nil
}
