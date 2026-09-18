package network

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrMieruEndpointConfigurationUnavailable = errors.New("Mieru endpoint configuration reconciliation unavailable")
	ErrMieruEndpointConfigurationConflict    = errors.New("Mieru endpoint configuration changed")
)

type MieruEndpointConfigurationSnapshot struct {
	ID                  uint
	NodeID              uint
	ServerCiphertext    string
	ClientConfig        string
	MieruPrincipalReady bool
}

type MieruEndpointConfigurationUpdate struct {
	ID                       uint
	ExpectedServerCiphertext string
	ExpectedClientConfig     string
	ServerCiphertext         string
	ClientConfig             string
}

type MieruEndpointConfigurationResult struct {
	Endpoints          []MieruEndpointConfigurationSnapshot
	ChangedEndpointIDs []uint
}

type MieruEndpointConfigurationRepository interface {
	ListMieruEndpointConfigurations(context.Context) ([]MieruEndpointConfigurationSnapshot, error)
	CommitMieruEndpointConfigurations(context.Context, []MieruEndpointConfigurationUpdate) error
}

type MieruEndpointConfigurations struct {
	Repository  MieruEndpointConfigurationRepository
	Cipher      ProviderCredentialCipher
	NewPassword func() (string, error)
}

func (s MieruEndpointConfigurations) Reconcile(ctx context.Context) (MieruEndpointConfigurationResult, error) {
	if s.Repository == nil || s.Cipher == nil {
		return MieruEndpointConfigurationResult{}, ErrMieruEndpointConfigurationUnavailable
	}
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.reconcileOnce(ctx)
		if !errors.Is(err, ErrMieruEndpointConfigurationConflict) {
			return result, err
		}
	}
	return MieruEndpointConfigurationResult{}, ErrMieruEndpointConfigurationConflict
}

func (s MieruEndpointConfigurations) reconcileOnce(ctx context.Context) (MieruEndpointConfigurationResult, error) {
	rows, err := s.Repository.ListMieruEndpointConfigurations(ctx)
	if err != nil {
		return MieruEndpointConfigurationResult{}, err
	}
	result := MieruEndpointConfigurationResult{Endpoints: rows}
	updates := make([]MieruEndpointConfigurationUpdate, 0)
	for _, row := range rows {
		serverRaw, err := s.Cipher.Decrypt(row.ServerCiphertext)
		if err != nil {
			return MieruEndpointConfigurationResult{}, fmt.Errorf("decrypt Mieru endpoint %d: %w", row.ID, err)
		}
		normalizedServer, normalizedClient, err := normalizeMieruEndpointConfigs(serverRaw, row.ClientConfig, serverRaw, s.password)
		if err != nil {
			return MieruEndpointConfigurationResult{}, fmt.Errorf("normalize Mieru endpoint %d: %w", row.ID, err)
		}
		if normalizedServer == serverRaw && normalizedClient == row.ClientConfig {
			continue
		}
		serverCiphertext, err := s.Cipher.Encrypt(normalizedServer)
		if err != nil {
			return MieruEndpointConfigurationResult{}, fmt.Errorf("encrypt Mieru endpoint %d: %w", row.ID, err)
		}
		updates = append(updates, MieruEndpointConfigurationUpdate{
			ID: row.ID, ExpectedServerCiphertext: row.ServerCiphertext, ExpectedClientConfig: row.ClientConfig,
			ServerCiphertext: serverCiphertext, ClientConfig: normalizedClient,
		})
		result.ChangedEndpointIDs = append(result.ChangedEndpointIDs, row.ID)
	}
	if len(updates) > 0 {
		if err := s.Repository.CommitMieruEndpointConfigurations(ctx, updates); err != nil {
			return MieruEndpointConfigurationResult{}, err
		}
	}
	return result, nil
}

func NormalizeMieruEndpointConfigs(serverRaw, clientRaw, existingRaw string) (string, string, error) {
	return normalizeMieruEndpointConfigs(serverRaw, clientRaw, existingRaw, newMieruEndpointPassword)
}

func normalizeMieruEndpointConfigs(serverRaw, clientRaw, existingRaw string, password func() (string, error)) (string, string, error) {
	var server map[string]any
	if err := json.Unmarshal([]byte(serverRaw), &server); err != nil || server == nil {
		return "", "", &ProtocolEndpointMutationValidation{Message: "协议配置校验失败。", Fields: map[string]string{"config": "服务端配置必须是 JSON 对象。"}}
	}
	var client map[string]any
	if err := json.Unmarshal([]byte(clientRaw), &client); err != nil || client == nil {
		return "", "", &ProtocolEndpointMutationValidation{Message: "协议配置校验失败。", Fields: map[string]string{"client_config": "客户端配置必须是 JSON 对象。"}}
	}

	secret := mieruEndpointPassword(existingRaw)
	if secret == "" {
		var err error
		secret, err = password()
		if err != nil {
			return "", "", err
		}
	}
	server["type"] = "mieru"
	server["users"] = []any{map[string]any{"password": secret}}
	client["type"] = "mieru"
	delete(client, "username")
	delete(client, "password")
	serverPayload, err := json.Marshal(server)
	if err != nil {
		return "", "", err
	}
	clientPayload, err := json.Marshal(client)
	if err != nil {
		return "", "", err
	}
	return string(serverPayload), string(clientPayload), nil
}

func mieruEndpointPassword(raw string) string {
	var server map[string]any
	if json.Unmarshal([]byte(raw), &server) != nil {
		return ""
	}
	users, _ := server["users"].([]any)
	if len(users) == 0 {
		return ""
	}
	user, _ := users[0].(map[string]any)
	secret, _ := user["password"].(string)
	return strings.TrimSpace(secret)
}

func newMieruEndpointPassword() (string, error) {
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(entropy), nil
}

func (s MieruEndpointConfigurations) password() (string, error) {
	if s.NewPassword != nil {
		return s.NewPassword()
	}
	return newMieruEndpointPassword()
}
