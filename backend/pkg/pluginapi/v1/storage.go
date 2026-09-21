package pluginv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// HostStorage addresses only the current plugin's encrypted, versioned namespace.
// It is available to active authorized server plugins outside lifecycle RPCs.
// A 503 response means a lifecycle transaction is in progress; retry later.
// A revision conflict requires reading again, not blindly retrying the write.
type HostStorage struct {
	client *http.Client
	token  string
}
type StoredValue struct {
	Revision uint64          `json:"revision"`
	Found    bool            `json:"found"`
	Value    json.RawMessage `json:"value,omitempty"`
}

func HostStorageFromEnvironment() (*HostStorage, error) {
	socket, token := os.Getenv("ZBOARD_PLUGIN_HOST_SOCKET"), os.Getenv("ZBOARD_PLUGIN_HOST_TOKEN")
	if !filepath.IsAbs(socket) || len(token) != 64 {
		return nil, errors.New("host storage is not granted")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 1}
	return &HostStorage{client: &http.Client{Transport: transport, Timeout: 5 * time.Second}, token: token}, nil
}
func (s *HostStorage) Close() { s.client.CloseIdleConnections() }
func (s *HostStorage) Get(ctx context.Context, key string) (StoredValue, error) {
	return s.call(ctx, "storage.get", key, 0, nil)
}
func (s *HostStorage) Put(ctx context.Context, key string, revision uint64, value json.RawMessage) (StoredValue, error) {
	return s.call(ctx, "storage.put", key, revision, value)
}
func (s *HostStorage) Delete(ctx context.Context, key string, revision uint64) (StoredValue, error) {
	return s.call(ctx, "storage.delete", key, revision, nil)
}
func (s *HostStorage) call(ctx context.Context, kind, key string, revision uint64, value json.RawMessage) (StoredValue, error) {
	var result StoredValue
	raw, err := json.Marshal(struct {
		Type     string          `json:"type"`
		Key      string          `json:"key"`
		Revision uint64          `json:"revision"`
		Value    json.RawMessage `json:"value,omitempty"`
	}{kind, key, revision, value})
	if err != nil {
		return result, err
	}
	if len(raw) > 36<<10 {
		return result, errors.New("storage request too large")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://plugin-host/storage", bytes.NewReader(raw))
	if err != nil {
		return result, err
	}
	req.Header.Set("X-Plugin-Host", s.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return result, fmt.Errorf("host storage denied or unavailable (HTTP %d)", response.StatusCode)
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
