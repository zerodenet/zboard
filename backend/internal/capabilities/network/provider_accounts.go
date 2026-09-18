package network

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrProviderConflict = errors.New("provider account changed")
var ErrProviderNotFound = errors.New("provider account not found")
var ErrProviderPermission = errors.New("provider administration requires current administrator")
var ErrProviderDuplicate = errors.New("provider account name already exists")

type ProviderValidation struct{ Fields map[string]string }

func (e *ProviderValidation) Error() string { return "供应商账户校验失败。" }

type ProviderAccount struct {
	ID               uint       `json:"id"`
	ProviderKey      string     `json:"provider_key"`
	Name             string     `json:"name"`
	Capabilities     []string   `json:"capabilities"`
	CredentialPrefix string     `json:"credential_prefix"`
	Status           string     `json:"status"`
	LastVerifiedAt   *time.Time `json:"last_verified_at,omitempty"`
	LastError        string     `json:"last_error"`
	Revision         uint64     `json:"revision"`
	CreatedBy        uint       `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	UsageCount       int64      `json:"usage_count"`
}
type ProviderSnapshot struct {
	Account    ProviderAccount
	Ciphertext string `json:"-"`
}
type ProviderChange struct {
	Name               string
	Ciphertext, Prefix string
	ReplaceCredential  bool
	Verify             bool
	Verified           bool
	Failure            string
	CheckedAt          time.Time
}
type ProviderAccountStore interface {
	LoadProvider(context.Context, uint, uint) (ProviderSnapshot, error)
	CommitProvider(context.Context, uint, ProviderSnapshot, ProviderChange) (ProviderAccount, error)
}
type ProviderCredentialCipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}
type ProviderCredentialVerifier interface {
	VerifyProviderCredential(context.Context, string, string) error
}
type ProviderDefinition struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}
type ProviderRegistry interface {
	ProviderDefinition(context.Context, string) (ProviderDefinition, error)
}
type ProviderAccounts struct {
	Store    ProviderAccountStore
	Cipher   ProviderCredentialCipher
	Verifier ProviderCredentialVerifier
}
type ProviderUpdate struct {
	Name             string `json:"name"`
	APIToken         string `json:"api_token"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

func (s ProviderAccounts) Update(ctx context.Context, actor, id uint, input ProviderUpdate) (ProviderAccount, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.APIToken = strings.TrimSpace(input.APIToken)
	fields := map[string]string{}
	if input.Name == "" || len(input.Name) > 80 {
		fields["name"] = "账户名称需要包含 1–80 个 UTF-8 字节。"
	}
	if input.APIToken != "" && len(input.APIToken) > 4096 {
		fields["api_token"] = "供应商凭据不能超过 4096 个 UTF-8 字节，或留空保留原凭据。"
	}
	if len(fields) > 0 {
		return ProviderAccount{}, &ProviderValidation{fields}
	}
	before, err := s.Store.LoadProvider(ctx, actor, id)
	if err != nil {
		return ProviderAccount{}, err
	}
	if before.Account.Revision != input.ExpectedRevision {
		return ProviderAccount{}, ErrProviderConflict
	}
	change := ProviderChange{Name: input.Name}
	if input.APIToken != "" {
		if err := s.Verifier.VerifyProviderCredential(ctx, before.Account.ProviderKey, input.APIToken); err != nil {
			return ProviderAccount{}, &ProviderValidation{map[string]string{"api_token": "新 Token 验证失败，原凭据已保留。请检查 Token 及所需权限。"}}
		}
		encrypted, err := s.Cipher.Encrypt(input.APIToken)
		if err != nil {
			return ProviderAccount{}, err
		}
		change.Ciphertext = encrypted
		change.Prefix = providerCredentialPrefix(input.APIToken)
		change.ReplaceCredential = true
		change.Verify = true
		change.Verified = true
		change.CheckedAt = time.Now().UTC()
	}
	return s.Store.CommitProvider(ctx, actor, before, change)
}

func (s ProviderAccounts) Verify(ctx context.Context, actor, id uint) (ProviderAccount, error) {
	before, err := s.Store.LoadProvider(ctx, actor, id)
	if err != nil {
		return ProviderAccount{}, err
	}
	token, err := s.Cipher.Decrypt(before.Ciphertext)
	if err != nil {
		return before.Account, err
	}
	verificationErr := s.Verifier.VerifyProviderCredential(ctx, before.Account.ProviderKey, token)
	if ctx.Err() != nil {
		return before.Account, ctx.Err()
	}
	change := ProviderChange{Name: before.Account.Name, Verify: true, Verified: verificationErr == nil, CheckedAt: time.Now().UTC()}
	if verificationErr != nil {
		change.Failure = strings.TrimSpace(verificationErr.Error())
		if len(change.Failure) > 4000 {
			change.Failure = change.Failure[:4000]
		}
	}
	result, err := s.Store.CommitProvider(ctx, actor, before, change)
	if err != nil {
		return before.Account, err
	}
	return result, verificationErr
}
