package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
}

var pluginVersion = "1.0.0"

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{
		Id: "example.route", Version: pluginVersion, Protocol: 1,
		Capabilities: []string{"zboard.config.v1", "zboard.http.route.v1"},
	}, nil
}

func (*server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}

func (*server) ValidateConfig(_ context.Context, request *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	return &pluginv1.ConfigResult{NormalizedJson: request.ConfigJson}, nil
}

func (*server) ApplyConfig(context.Context, *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}

func (*server) HandleHTTP(_ context.Context, request *pluginv1.HTTPRequest) (*pluginv1.HTTPResponse, error) {
	if request.RouteId == "notification" && request.Method == "POST" && request.Path == "/.well-known/example/v1/notification" {
		if _, err := url.ParseQuery(request.RawQuery); err != nil {
			return &pluginv1.HTTPResponse{Status: 400, ContentType: "text/plain", Body: []byte("invalid query")}, nil
		}
		mac := hmac.New(sha256.New, []byte("fixture-route-secret"))
		mac.Write([]byte(request.RequestUri + "\n"))
		mac.Write(request.Body)
		signatures := request.Headers["X-Webhook-Signature"].GetValues()
		if len(signatures) != 1 || !hmac.Equal(signatures[0], []byte(hex.EncodeToString(mac.Sum(nil)))) {
			return &pluginv1.HTTPResponse{Status: 401, ContentType: "text/plain", Body: []byte("invalid signature")}, nil
		}
		// Reflect the received contract so tests verify the actual process hop.
		body, err := protojson.Marshal(request)
		return &pluginv1.HTTPResponse{Status: 200, ContentType: "application/json", Body: body}, err
	}
	valid := request.Method == "GET" && ((request.RouteId == "discovery" && request.Path == "/.well-known/example/v1/discovery") ||
		(request.RouteId == "replacement" && request.Path == "/.well-known/example/v2/discovery"))
	if !valid {
		return &pluginv1.HTTPResponse{Status: 404, ContentType: "application/json", Body: []byte(`{"error":"not_found"}`)}, nil
	}
	if request.Headers["X-Test-Body-Sha256"] != nil {
		expected := request.Headers["X-Test-Body-Sha256"].GetValues()
		digest := sha256.Sum256(request.Body)
		if len(expected) != 1 || string(expected[0]) != hex.EncodeToString(digest[:]) {
			return &pluginv1.HTTPResponse{Status: 400, ContentType: "text/plain", Body: []byte("body changed")}, nil
		}
		return &pluginv1.HTTPResponse{Status: 200, ContentType: "text/plain", Body: []byte("body verified")}, nil
	}
	return &pluginv1.HTTPResponse{Status: 200, ContentType: "application/json", Body: []byte(`{"available":true}`)}, nil
}

func main() { pluginv1.Serve(&server{}) }
