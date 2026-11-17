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
	"gorm.io/gorm"
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
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 35 * time.Second, MaxHeaderBytes: 4096}
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
	if DecodeStrict(raw, &request) != nil || !hostOperationPattern.MatchString(request.Operation) || len(request.Payload) == 0 || !json.Valid(request.Payload) {
		nativeServiceError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !isHostServiceCapability(request.Capability) {
		nativeServiceError(w, http.StatusServiceUnavailable, "capability_unavailable")
		return
	}
	if (request.Capability == CommerceOrdersReadCapability || request.Capability == CommercePaymentsWriteCapability) && len(request.Payload) > MaxPageActionBytes {
		nativeServiceError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !m.mu.TryLock() {
		nativeServiceError(w, http.StatusServiceUnavailable, "temporary_unavailable")
		return
	}
	current, loadErr := m.load(v.ID)
	if loadErr != nil {
		m.mu.Unlock()
		if errors.Is(loadErr, gorm.ErrRecordNotFound) && current.ID == "" {
			nativeServiceError(w, http.StatusForbidden, "forbidden")
			return
		}
		nativeServiceError(w, http.StatusServiceUnavailable, "temporary_unavailable")
		return
	}
	active := loadErr == nil && !m.lost.Load() && time.Now().UnixNano() < m.leaseUntil.Load() && current.Enabled && current.State == "active" && current.VersionID == v.VersionID && current.SigningKey == v.SigningKey && proc != nil && m.processes[v.ID] == proc && hasCapability(current, request.Capability)
	commerceCall := request.Capability == CommerceOrdersReadCapability || request.Capability == CommercePaymentsWriteCapability
	var admissionErr error
	if active && commerceCall {
		limit := 60
		if request.Capability == CommercePaymentsWriteCapability {
			limit = 120
		}
		admissionErr = m.admitPluginCall("native:"+paymentIdentity(current)+":"+request.Operation, limit)
	}
	services := m.services
	if !active || !commerceCall {
		m.mu.Unlock()
	} else {
		defer m.mu.Unlock()
	}
	if admissionErr != nil {
		code := "rate_limited"
		if errors.Is(admissionErr, ErrUnavailable) {
			code = "temporary_unavailable"
		}
		_ = json.NewEncoder(w).Encode(pluginv1.HostCallResponse{Error: &pluginv1.HostCallError{Code: code}})
		return
	}
	if !active {
		nativeServiceError(w, http.StatusForbidden, "forbidden")
		return
	}
	if request.Capability == HostDiscoveryCapability {
		var empty struct{}
		if request.Operation != "capabilities.list" || DecodeStrict(request.Payload, &empty) != nil {
			http.Error(w, "invalid discovery request", http.StatusBadRequest)
			return
		}
		data, _ := json.Marshal(map[string]any{
			"host_version": m.host, "plugin_protocol": v.Manifest.Requires.Protocol,
			"ui_bridge": v.Manifest.Requires.Bridge, "capabilities": v.Admission.Capabilities,
		})
		_ = json.NewEncoder(w).Encode(pluginv1.HostCallResponse{Data: data})
		return
	}
	if services == nil {
		nativeServiceError(w, http.StatusServiceUnavailable, "capability_unavailable")
		return
	}
	callCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if commerceCall {
		callCtx = context.WithValue(callCtx, nativeIdentityKey{}, paymentIdentity(current))
	}
	data, callErr := services.CallPluginHost(callCtx, v.ID, request.Capability, request.Operation, request.Payload)
	response := pluginv1.HostCallResponse{Data: data, Error: callErr}
	if response.Data == nil && response.Error == nil {
		response.Data = json.RawMessage(`{}`)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (m *Manager) handleHostStorage(w http.ResponseWriter, r *http.Request, v Installation, proc *process, raw []byte) {
	var request StorageRequest
	if DecodeStrict(raw, &request) != nil {
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

func nativeServiceError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(pluginv1.HostCallResponse{Error: &pluginv1.HostCallError{Code: code}})
}
