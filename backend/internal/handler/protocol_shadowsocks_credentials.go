package handler

import (
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const shadowsocks2022ChachaCipher = "2022-blake3-chacha20-poly1305"

func shadowsocksUsesSIP023Identity(cipher string) bool {
	switch strings.ToLower(strings.TrimSpace(cipher)) {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm":
		return true
	default:
		return false
	}
}

func shadowsocksManagedUsersSharePort(cipher string) bool {
	return !strings.EqualFold(strings.TrimSpace(cipher), shadowsocks2022ChachaCipher)
}

func (h *handlers) managedShadowsocksRuntimeProtocol(protocol map[string]interface{}, contexts []runtimeCredentialContext) error {
	cipherName, _ := protocol["cipher"].(string)
	if !shadowsocksManagedUsersSharePort(cipherName) && len(contexts) > 1 {
		return fmt.Errorf("Shadowsocks cipher %s cannot identify multiple managed users on one endpoint port; use a legacy AEAD cipher or a 2022 AES cipher", strings.TrimSpace(cipherName))
	}

	users := make([]interface{}, 0, len(contexts))
	for _, context := range contexts {
		secret, err := h.credentialCipher.Decrypt(context.Credential.Secret)
		if err != nil {
			return fmt.Errorf("decrypt protocol credential %d: %w", context.Credential.ID, err)
		}
		user, err := managedAccessUserFields(context)
		if err != nil {
			return err
		}
		user["password"] = secret
		users = append(users, user)
	}
	return configureManagedShadowsocksUsers(protocol, cipherName, users)
}

func (h *handlers) legacyShadowsocksRuntimeProtocol(protocol map[string]interface{}, credentials []model.ProtocolCredential) error {
	cipherName, _ := protocol["cipher"].(string)
	if !shadowsocksManagedUsersSharePort(cipherName) && len(credentials) > 1 {
		return fmt.Errorf("Shadowsocks cipher %s cannot identify multiple managed users on one endpoint port; use a legacy AEAD cipher or a 2022 AES cipher", strings.TrimSpace(cipherName))
	}

	users := make([]interface{}, 0, len(credentials))
	for _, credential := range credentials {
		secret, err := h.credentialCipher.Decrypt(credential.Secret)
		if err != nil {
			return fmt.Errorf("decrypt protocol credential %d: %w", credential.ID, err)
		}
		users = append(users, map[string]interface{}{
			"password":      secret,
			"principal_key": credential.PrincipalKey,
		})
	}
	return configureManagedShadowsocksUsers(protocol, cipherName, users)
}

func configureManagedShadowsocksUsers(protocol map[string]interface{}, cipherName string, users []interface{}) error {
	if shadowsocksUsesSIP023Identity(cipherName) {
		identity, _ := protocol["password"].(string)
		identity = strings.TrimSpace(identity)
		if identity == "" {
			return errorsForShadowsocksIdentity(cipherName)
		}
		for _, rawUser := range users {
			user, _ := rawUser.(map[string]interface{})
			password, _ := user["password"].(string)
			if password == identity {
				return fmt.Errorf("managed Shadowsocks user reuses the endpoint identity password")
			}
		}
		protocol["identity_password"] = identity
	} else {
		delete(protocol, "identity_password")
	}
	delete(protocol, "password")
	protocol["users"] = users
	return nil
}

func errorsForShadowsocksIdentity(cipherName string) error {
	return fmt.Errorf("Shadowsocks cipher %s requires a stable endpoint identity password", strings.TrimSpace(cipherName))
}

func protocolCredentialClientPort(endpoint model.ProtocolEndpoint, credential model.ProtocolCredential) int {
	if strings.EqualFold(strings.TrimSpace(endpoint.Protocol), "shadowsocks") {
		return endpoint.PublicPort
	}
	return credential.PublicPort
}
