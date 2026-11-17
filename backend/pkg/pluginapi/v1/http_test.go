package pluginv1

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestHTTPRequestCapturesRawInputAndSnapshotsMutableValues(t *testing.T) {
	body := []byte("{ \"event\": \"a\" }\r\n\x00")
	r := httptest.NewRequest("POST", "http://origin.example/.well-known/example/%6Eotification?x=one&x=two&empty=&encoded=%2f+%20&bad=%ZZ", bytes.NewReader(body))
	r.Header = http.Header{"Content-Type": {"application/json; charset=utf-8"}, "Authorization": {"Bearer route-credential"}, "Cookie": {"a=1", "b=2"}, "X-Signature": {"first", "", "third"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-For": {"203.0.113.1"}}
	r.RequestURI = r.URL.RequestURI()
	r.RemoteAddr = "192.0.2.1:1234"
	r.Proto = "HTTP/2.0"
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	r.Trailer = http.Header{"X-Trailer-Signature": {"after-body"}}
	request := NewHTTPRequest(r, body)
	if request.RouteId != "" || request.Path != "/.well-known/example/notification" || request.RawPath != "/.well-known/example/%6Eotification" || request.RawQuery != r.URL.RawQuery || request.RequestUri != r.RequestURI || request.Host != r.Host || request.RemoteAddr != r.RemoteAddr || request.Scheme != "http" || request.Proto != "HTTP/2.0" || request.ContentLength != -1 {
		t.Fatalf("request metadata was lost or rewritten: %+v", request)
	}
	if !bytes.Equal(request.Body, body) || len(request.Headers["X-Signature"].Values) != 3 || string(request.Headers["Authorization"].Values[0]) != "Bearer route-credential" || string(request.Trailers["X-Trailer-Signature"].Values[0]) != "after-body" {
		t.Fatalf("request input lost: %+v", request)
	}
	body[0] = '!'
	r.Header["X-Signature"][0] = "changed"
	r.TransferEncoding[0] = "changed"
	r.Trailer["X-Trailer-Signature"][0] = "changed"
	if request.Body[0] != '{' || string(request.Headers["X-Signature"].Values[0]) != "first" || request.TransferEncoding[0] != "chunked" || string(request.Trailers["X-Trailer-Signature"].Values[0]) != "after-body" {
		t.Fatal("request retained mutable HTTP buffers")
	}
	encoded, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded := new(HTTPRequest)
	if err := proto.Unmarshal(encoded, decoded); err != nil || !proto.Equal(request, decoded) {
		t.Fatalf("wire contract mismatch: %v", err)
	}
}

func TestHTTPRequestCapturesEmptyQueryAndTLSAuthenticationMetadata(t *testing.T) {
	r := httptest.NewRequest("GET", "https://origin.example/.well-known/example/status?", nil)
	r.RequestURI = ""
	certificate := &x509.Certificate{Raw: []byte("certificate DER")}
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13, CipherSuite: tls.TLS_AES_128_GCM_SHA256, ServerName: "origin.example", NegotiatedProtocol: "h2", HandshakeComplete: true, PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}}}
	request := NewHTTPRequest(r, nil)
	if !request.ForceQuery || request.RawQuery != "" || request.RequestUri != "/.well-known/example/status?" || request.Scheme != "https" || request.Tls.Version != tls.VersionTLS13 || request.Tls.NegotiatedProtocol != "h2" {
		t.Fatalf("transport metadata lost: %+v", request)
	}
	certificate.Raw[0] = '!'
	if string(request.Tls.PeerCertificates[0]) != "certificate DER" || string(request.Tls.VerifiedChains[0].Certificates[0]) != "certificate DER" {
		t.Fatal("TLS certificates were not snapshotted")
	}
}

func TestHTTPRequestCapturesTrailersAfterBodyEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		request := NewHTTPRequest(r, body)
		if string(request.Body) != "payload" || string(request.Trailers["X-Signature"].GetValues()[0]) != "trailer-signature" || request.ContentLength != -1 || len(request.TransferEncoding) != 1 {
			t.Errorf("stream metadata lost: %+v", request)
		}
		if !bytes.Equal(request.Headers["X-Opaque"].GetValues()[0], []byte{0xff, 0x80}) || request.ContentType != "" || !bytes.Equal(request.Headers["Content-Type"].GetValues()[0], []byte{'x', 0xff}) {
			t.Errorf("opaque header bytes lost: %+v", request)
		}
		if _, err := proto.Marshal(request); err != nil {
			t.Errorf("opaque HTTP input cannot cross protobuf transport: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	r, _ := http.NewRequest("POST", server.URL, io.NopCloser(bytes.NewBufferString("payload")))
	r.Trailer = http.Header{"X-Signature": {"trailer-signature"}}
	r.Header["X-Opaque"] = []string{string([]byte{0xff, 0x80})}
	r.Header["Content-Type"] = []string{string([]byte{'x', 0xff})}
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestHTTPRequestRetainsOriginalV1WireFields(t *testing.T) {
	// Decode against the original five-field schema, rather than the current
	// generated type, to verify both directions of SDK compatibility.
	message := &descriptorpb.DescriptorProto{Name: proto.String("HTTPRequest")}
	for i, name := range []string{"route_id", "method", "path", "content_type", "body"} {
		kind := descriptorpb.FieldDescriptorProto_TYPE_STRING
		if name == "body" {
			kind = descriptorpb.FieldDescriptorProto_TYPE_BYTES
		}
		message.Field = append(message.Field, &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), Number: proto.Int32(int32(i + 1)), Type: &kind,
			Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		})
	}
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: proto.String("original_http.proto"), Syntax: proto.String("proto3"), MessageType: []*descriptorpb.DescriptorProto{message},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	original := dynamicpb.NewMessage(file.Messages().Get(0))
	want := &HTTPRequest{RouteId: "notification", Method: "POST", Path: "/callback", ContentType: "application/json", Body: []byte{0, 0xff}}
	expanded := proto.Clone(want).(*HTTPRequest)
	expanded.RawQuery = "nonce=1&nonce=2"
	expanded.Headers = map[string]*HTTPHeaderValues{"X-Signature": {Values: [][]byte{[]byte("signature")}}}
	wire, err := proto.Marshal(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(wire, original); err != nil {
		t.Fatal(err)
	}
	wire, err = proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	got := new(HTTPRequest)
	if err := proto.Unmarshal(wire, got); err != nil || !proto.Equal(got, want) {
		t.Fatalf("original v1 wire contract changed: %v, got %v, want %v", err, got, want)
	}
}
