package plugins

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

var hostOperationPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*){1,3}$`)

const nativeStorageValueBytes = 8 << 20
const nativeStorageBytes = 32 << 20

// Each process receives only its own private socket and bearer token. There is
// deliberately no plugin ID, user, SQL, or core-command selector in this API.
func (m *Manager) startAuthorizedProcess(ctx context.Context, v Installation) (*process, error) {
	if err := executionAuthorized(v); err != nil {
		return nil, err
	}
	pack, err := m.packageFor(v)
	if err != nil {
		return nil, err
	}
	if !requiresHostCallback(v) {
		return startProcess(ctx, m.options.Directory, pack)
	}
	directory, err := os.MkdirTemp("", "zbh-")
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(directory, "host.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(directory)
		return nil, err
	}
	if err := os.Chmod(socket, 0600); err != nil {
		listener.Close()
		os.RemoveAll(directory)
		return nil, err
	}
	token := randomToken()
	var proc *process
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxHeaderBytes: 4096}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "POST" || (r.URL.Path != "/storage" && r.URL.Path != "/service") || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Plugin-Host")), []byte(token)) != 1 {
			http.Error(w, "forbidden", 403)
			return
		}
		limit := int64(nativeStorageValueBytes + 4096)
		if r.URL.Path == "/service" {
			limit = pluginv1.MaxHostCallBytes + 4096
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if r.URL.Path == "/service" {
			var request pluginv1.HostCallRequest
			if DecodeStrict(raw, &request) != nil || !isHostServiceCapability(request.Capability) || !hostOperationPattern.MatchString(request.Operation) || len(request.Payload) == 0 || !json.Valid(request.Payload) {
				http.Error(w, "invalid request", 400)
				return
			}
			if !m.mu.TryLock() {
				http.Error(w, "host busy; retry outside lifecycle callback", 503)
				return
			}
			active := proc != nil && m.processes[v.ID] == proc && hasCapability(v, request.Capability)
			services := m.services
			m.mu.Unlock()
			if !active {
				http.Error(w, "plugin process is not active", 403)
				return
			}
			if services == nil {
				http.Error(w, "host services unavailable", 503)
				return
			}
			data, callErr := services.CallPluginHost(r.Context(), v.ID, request.Capability, request.Operation, request.Payload)
			response := pluginv1.HostCallResponse{Data: data, Error: callErr}
			if response.Data == nil && response.Error == nil {
				response.Data = json.RawMessage(`{}`)
			}
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		var request StorageRequest
		if DecodeStrict(raw, &request) != nil || !hasCapability(v, StorageCapability) {
			http.Error(w, "invalid request", 400)
			return
		}
		// Never deadlock a lifecycle RPC whose plugin synchronously calls back.
		// Storage is unavailable during configuration/health/identity transactions.
		if !m.mu.TryLock() {
			http.Error(w, "host busy; retry outside lifecycle callback", 503)
			return
		}
		defer m.mu.Unlock()
		if proc == nil || m.processes[v.ID] != proc {
			http.Error(w, "plugin process is not active", 403)
			return
		}
		result, err := m.storageLocked(v.ID, request, nativeStorageValueBytes, nativeStorageBytes)
		if err != nil {
			code := 400
			if errors.Is(err, ErrPermission) {
				code = 403
			}
			if errors.Is(err, ErrConflict) {
				code = 409
			}
			http.Error(w, err.Error(), code)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	closeHost := func() { _ = server.Close(); _ = listener.Close(); _ = os.RemoveAll(directory) }
	go func() { _ = server.Serve(listener) }()
	proc, err = startProcess(ctx, m.options.Directory, pack, "ZBOARD_PLUGIN_HOST_SOCKET="+socket, "ZBOARD_PLUGIN_HOST_TOKEN="+token)
	if err != nil {
		closeHost()
		return nil, err
	}
	proc.closeHost = closeHost
	return proc, nil
}
