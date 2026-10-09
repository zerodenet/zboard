package pluginv1

import (
	"bytes"
	"crypto/x509"
	"net/http"
	"unicode/utf8"
)

const (
	MaxHTTPBodyBytes       = 8 << 20
	MaxHTTPMetadataBytes   = 256 << 10
	MaxControlMessageBytes = 9 << 20
)

// NewHTTPRequest captures a server request after its body has been read to EOF,
// so trailers are populated. It preserves application body bytes and raw query
// encoding, and snapshots mutable data. RouteId is assigned only by the host's
// admitted route registry. Forwarded headers are never used as transport facts.
func NewHTTPRequest(r *http.Request, body []byte) *HTTPRequest {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	requestURI := r.RequestURI
	if requestURI == "" {
		requestURI = r.URL.RequestURI()
	}
	request := &HTTPRequest{
		Method: r.Method, Path: r.URL.Path, ContentType: r.Header.Get("Content-Type"), Body: bytes.Clone(body),
		RawQuery: r.URL.RawQuery, Headers: captureHTTPHeaders(r.Header), Host: r.Host,
		RequestUri: requestURI, RawPath: r.URL.RawPath, Scheme: scheme, RemoteAddr: r.RemoteAddr,
		Proto: r.Proto, ContentLength: r.ContentLength, TransferEncoding: append([]string(nil), r.TransferEncoding...),
		Trailers: captureHTTPHeaders(r.Trailer), ForceQuery: r.URL.ForceQuery,
	}
	// The original compatibility field is a protobuf string. Opaque HTTP values
	// remain available losslessly in Headers even when they cannot fit that field.
	if !utf8.ValidString(request.ContentType) {
		request.ContentType = ""
	}
	if state := r.TLS; state != nil {
		request.Tls = &HTTPConnectionTLS{
			Version: uint32(state.Version), CipherSuite: uint32(state.CipherSuite), ServerName: state.ServerName,
			NegotiatedProtocol: state.NegotiatedProtocol, HandshakeComplete: state.HandshakeComplete, DidResume: state.DidResume,
			PeerCertificates: captureHTTPCertificates(state.PeerCertificates),
		}
		for _, chain := range state.VerifiedChains {
			request.Tls.VerifiedChains = append(request.Tls.VerifiedChains, &HTTPCertificateChain{Certificates: captureHTTPCertificates(chain)})
		}
	}
	return request
}

func captureHTTPHeaders(headers http.Header) map[string]*HTTPHeaderValues {
	result := make(map[string]*HTTPHeaderValues, len(headers))
	for name, values := range headers {
		field := &HTTPHeaderValues{Values: make([][]byte, len(values))}
		for i, value := range values {
			field.Values[i] = []byte(value)
		}
		result[name] = field
	}
	return result
}

func captureHTTPCertificates(certificates []*x509.Certificate) [][]byte {
	result := make([][]byte, 0, len(certificates))
	for _, certificate := range certificates {
		if certificate != nil {
			result = append(result, bytes.Clone(certificate.Raw))
		}
	}
	return result
}
