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

const MaxHostCallBytes = 8 << 20

const (
	AccountAssertionCapability       = "zboard.account.assertion.v1"
	SubscriptionProjectionCapability = "zboard.subscription.projection.v1"
	MessageProjectionCapability      = "zboard.message.projection.v1"
)

type AccountAssertionRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type Principal struct {
	// ID is a host-signed, plugin-scoped identity. Pass it unchanged as principal_id.
	ID      string `json:"id"`
	Display string `json:"display"`
	Admin   bool   `json:"admin"`
}

type SubscriptionListRequest struct {
	PrincipalID string `json:"principal_id"`
	Format      string `json:"format"`
}

type SubscriptionContentRequest struct {
	PrincipalID    string `json:"principal_id"`
	SubscriptionID string `json:"subscription_id"`
	Format         string `json:"format"`
	KnownRevision  string `json:"known_revision,omitempty"`
}

type SubscriptionUsageRequest struct {
	PrincipalID    string `json:"principal_id"`
	SubscriptionID string `json:"subscription_id"`
}

type SubscriptionCapabilities struct {
	Usage bool `json:"usage"`
}

type ProjectedSubscription struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	Format        string `json:"format"`
	Revision      string `json:"revision"`
	ContentSHA256 string `json:"content_sha256"`
	UpdatedAt     int64  `json:"updated_at"`
	Content       string `json:"content,omitempty"`
	NotModified   bool   `json:"not_modified,omitempty"`
}

// SubscriptionUsage is account-scoped quota metadata, independent of the
// rendered configuration and its revision. UsedBytes is an aggregate; it does
// not imply an upload/download split.
type SubscriptionUsage struct {
	UsedBytes      uint64 `json:"used_bytes"`
	TotalBytes     uint64 `json:"total_bytes"`
	ExpireAtUnixMs uint64 `json:"expire_at_unix_ms"`
}

type MessageListRequest struct {
	PrincipalID string `json:"principal_id"`
	Cursor      string `json:"cursor,omitempty"`
	Limit       int    `json:"limit"`
}

type MessageGetRequest struct {
	PrincipalID string `json:"principal_id"`
	MessageID   string `json:"message_id"`
}

type MessageMarkReadRequest struct {
	PrincipalID string `json:"principal_id"`
	MessageID   string `json:"message_id"`
	Revision    uint64 `json:"revision"`
}

type ProjectedMessage struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body,omitempty"`
	Severity    string `json:"severity"`
	Revision    uint64 `json:"revision"`
	PublishedAt int64  `json:"published_at"`
	UpdatedAt   int64  `json:"updated_at"`
	ReadAt      int64  `json:"read_at,omitempty"`
}

type MessagePage struct {
	Items      []ProjectedMessage `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type HostCallRequest struct {
	Capability string          `json:"capability"`
	Operation  string          `json:"operation"`
	Payload    json.RawMessage `json:"payload"`
}

type HostCallResponse struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Error *HostCallError  `json:"error,omitempty"`
}

type HostCallError struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *HostCallError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// HostClient reaches only the capability-scoped services admitted for the
// current plugin process. It has no plugin selector, SQL handle, or core API.
type HostClient struct {
	client *http.Client
	token  string
}

func HostClientFromEnvironment() (*HostClient, error) {
	socket, token := os.Getenv("ZBOARD_PLUGIN_HOST_SOCKET"), os.Getenv("ZBOARD_PLUGIN_HOST_TOKEN")
	if !filepath.IsAbs(socket) || len(token) != 64 {
		return nil, errors.New("plugin host services are not granted")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 2}
	return &HostClient{client: &http.Client{Transport: transport, Timeout: 10 * time.Second}, token: token}, nil
}

func (c *HostClient) Close() { c.client.CloseIdleConnections() }

func (c *HostClient) Call(ctx context.Context, capability, operation string, payload any, out any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(HostCallRequest{Capability: capability, Operation: operation, Payload: payloadJSON})
	if err != nil {
		return err
	}
	if len(raw) > MaxHostCallBytes {
		return errors.New("host call is too large")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://plugin-host/service", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("X-Plugin-Host", c.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("plugin host denied or unavailable (HTTP %d)", response.StatusCode)
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, MaxHostCallBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > MaxHostCallBytes {
		return errors.New("plugin host response is too large")
	}
	var result HostCallResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if result.Error != nil {
		return result.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(result.Data, out)
}
