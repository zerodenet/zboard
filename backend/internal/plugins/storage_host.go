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
	"time"
)

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
	if !hasCapability(v, StorageCapability) {
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
		if r.Method != "POST" || r.URL.Path != "/storage" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Plugin-Host")), []byte(token)) != 1 {
			http.Error(w, "forbidden", 403)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxStorageValueBytes+4096))
		var request StorageRequest
		if err != nil || DecodeStrict(raw, &request) != nil {
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
		result, err := m.storageLocked(v.ID, request)
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
