package network

import (
	"context"
	"slices"
	"strings"
)

type ProviderCreate struct {
	ProviderKey string `json:"provider_key"`
	Name        string `json:"name"`
	APIToken    string `json:"api_token"`
}
type ProviderCreationStore interface {
	CreateProvider(context.Context, uint, ProviderSnapshot) (ProviderAccount, error)
}
type ProviderCreation struct {
	Store    ProviderCreationStore
	Accounts ProviderAccounts
	Registry ProviderRegistry
}

// Creation preserves the established saved-but-unverified outcome. A provider
// verification failure never pretends the already committed account vanished.
func (s ProviderCreation) Create(ctx context.Context, actor uint, input ProviderCreate) (account ProviderAccount, verificationErr, errorResult error) {
	input.ProviderKey = strings.ToLower(strings.TrimSpace(input.ProviderKey))
	input.Name = strings.TrimSpace(input.Name)
	input.APIToken = strings.TrimSpace(input.APIToken)
	fields := map[string]string{}
	var definition ProviderDefinition
	var definitionErr error
	if s.Registry == nil {
		definitionErr = ErrManagedDNSDependency
	} else {
		definition, definitionErr = s.Registry.ProviderDefinition(ctx, input.ProviderKey)
	}
	if definitionErr != nil || definition.Key != input.ProviderKey || (!slices.Contains(definition.Capabilities, "dns.records") && !slices.Contains(definition.Capabilities, "certificate.issue")) {
		fields["provider_key"] = "请选择当前可用且支持 DNS 或证书签发的供应商。"
	}
	if input.Name == "" || len(input.Name) > 80 {
		fields["name"] = "账户名称需要包含 1–80 个 UTF-8 字节。"
	}
	if input.APIToken == "" || len(input.APIToken) > 4096 {
		fields["api_token"] = "请输入有效的供应商凭据（最多 4096 个 UTF-8 字节）。"
	}
	if len(fields) > 0 {
		return account, nil, &ProviderValidation{fields}
	}
	encrypted, err := s.Accounts.Cipher.Encrypt(input.APIToken)
	if err != nil {
		return account, nil, err
	}
	account, err = s.Store.CreateProvider(ctx, actor, ProviderSnapshot{Account: ProviderAccount{ProviderKey: input.ProviderKey, Name: input.Name, Capabilities: append([]string{}, definition.Capabilities...), CredentialPrefix: providerCredentialPrefix(input.APIToken), Status: "pending", Revision: 1, CreatedBy: actor}, Ciphertext: encrypted})
	if err != nil {
		return account, nil, err
	}
	verified, verificationErr := s.Accounts.Verify(ctx, actor, account.ID)
	if verified.ID != 0 {
		account = verified
	}
	return account, verificationErr, nil
}

func providerCredentialPrefix(value string) string {
	runes := []rune(value)
	if len(runes) <= 8 {
		return "••••"
	}
	return string(runes[:4]) + "…" + string(runes[len(runes)-4:])
}
