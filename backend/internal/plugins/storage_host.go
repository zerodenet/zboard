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

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

var hostOperationPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*){1,3}$`)

const nativeStorageValueBytes = 8 << 20
const nativeStorageBytes = 32 << 20

// Each process receives only its own private socket and bearer token. There is
// deliberately no caller-selected plugin ID, user, SQL, or core-command selector in this API.
func (m *Manager) startAuthorizedProcess(ctx context.Context, v Installation) (*process, error) {
	if err := executionAuthorized(v); err != nil {
		return nil, err
	}
	pack, err := m.packageFor(v)
	if err != nil {
		return nil, err
	}
	if !requiresHostCallback(v) && !hasCapability(v, TaskCapability) {
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
		if r.Method != http.MethodPost || !allowedHostCallbackPath(r.URL.Path) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Plugin-Host")), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		limit := int64(nativeStorageValueBytes + 4096)
		if r.URL.Path == "/tasks" {
			limit = 4096
		} else if r.URL.Path == "/service" {
			limit = pluginv1.MaxHostCallBytes + 4096
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/tasks":
			m.handleHostTask(w, r, v, proc, raw)
		case "/service":
			m.handleHostService(w, r, v, proc, raw)
		case "/storage":
			m.handleHostStorage(w, r, v, proc, raw)
		}
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

func allowedHostCallbackPath(path string) bool {
	return path == "/storage" || path == "/tasks" || path == "/service"
}

func (m *Manager) handleHostTask(w http.ResponseWriter, r *http.Request, v Installation, proc *process, raw []byte) {
	var request hostTaskRequest
	if DecodeStrict(raw, &request) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !m.mu.TryLock() {
		http.Error(w, "host busy; retry outside lifecycle callback", http.StatusServiceUnavailable)
		return
	}
	defer m.mu.Unlock()
	if proc == nil || m.processes[v.ID] != proc {
		http.Error(w, "plugin process is not active", http.StatusForbidden)
		return
	}
	result, err := m.hostTasksLocked(r.Context(), v.ID, request)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrPermission) {
			code = http.StatusForbidden
		}
		if errors.Is(err, ErrUnavailable) {
			code = http.StatusServiceUnavailable
		}
		if errors.Is(err, jobs.ErrBackpressure) {
			code = http.StatusTooManyRequests
			w.Header().Set("Retry-After", "5")
		}
		http.Error(w, "task request denied or unavailable", code)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (m *Manager) handleHostService(w http.ResponseWriter, r *http.Request, v Installation, proc *process, raw []byte) {
	var request pluginv1.HostCallRequest
	if DecodeStrict(raw, &request) != nil || !isHostServiceCapability(request.Capability) || !hostOperationPattern.MatchString(request.Operation) || len(request.Payload) == 0 || !json.Valid(request.Payload) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !m.mu.TryLock() {
		http.Error(w, "host busy; retry outside lifecycle callback", http.StatusServiceUnavailable)
		return
	}
	active := proc != nil && m.processes[v.ID] == proc && hasCapability(v, request.Capability)
	services := m.services
	m.mu.Unlock()
	if !active {
		http.Error(w, "plugin process is not active", http.StatusForbidden)
		return
	}
	if services == nil {
		http.Error(w, "host services unavailable", http.StatusServiceUnavailable)
		return
	}
	data, callErr := services.CallPluginHost(r.Context(), v.ID, request.Capability, request.Operation, request.Payload)
	response := pluginv1.HostCallResponse{Data: data, Error: callErr}
	if response.Data == nil && response.Error == nil {
		response.Data = json.RawMessage(`{}`)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (m *Manager) handleHostStorage(w http.ResponseWriter, r *http.Request, v Installation, proc *process, raw []byte) {
	var request StorageRequest
	if DecodeStrict(raw, &request) != nil || !hasCapability(v, StorageCapability) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Never deadlock a lifecycle RPC whose plugin synchronously calls back.
	if !m.mu.TryLock() {
		http.Error(w, "host busy; retry outside lifecycle callback", http.StatusServiceUnavailable)
		return
	}
	defer m.mu.Unlock()
	if proc == nil || m.processes[v.ID] != proc {
		http.Error(w, "plugin process is not active", http.StatusForbidden)
		return
	}
	result, err := m.storageLocked(v.ID, request, nativeStorageValueBytes, nativeStorageBytes)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrPermission) {
			code = http.StatusForbidden
		}
		if errors.Is(err, ErrConflict) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
