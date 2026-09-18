package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrIntegrationInput = errors.New("invalid integration credential request")

type IntegrationToken struct {
	ID        uint       `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"token_prefix"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}
type IntegrationIssue struct {
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
}
type IssuedIntegration struct {
	Credential IntegrationToken `json:"credential"`
	Token      string           `json:"token"`
}
type IntegrationRepository interface {
	Create(context.Context, uint, IntegrationIssue, string, string) (IntegrationToken, error)
	List(context.Context, uint, int, int) ([]IntegrationToken, error)
	Revoke(context.Context, uint, uint) error
}
type Integrations struct {
	Repository    IntegrationRepository
	AllowedScopes []string
}

func IntegrationTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s Integrations) Issue(ctx context.Context, actor uint, in IntegrationIssue) (IssuedIntegration, error) {
	if actor == 0 {
		return IssuedIntegration{}, ErrPermission
	}
	in.Name = strings.TrimSpace(in.Name)
	now := time.Now().UTC()
	if in.Name == "" || len(in.Name) > 120 || !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(366*24*time.Hour)) || len(in.Scopes) == 0 || len(in.Scopes) > 32 {
		return IssuedIntegration{}, ErrIntegrationInput
	}
	allowed := map[string]bool{}
	for _, scope := range s.AllowedScopes {
		allowed[scope] = true
	}
	seen := map[string]bool{}
	scopes := []string{}
	for _, scope := range in.Scopes {
		if !allowed[scope] {
			return IssuedIntegration{}, ErrIntegrationInput
		}
		if !seen[scope] {
			seen[scope] = true
			scopes = append(scopes, scope)
		}
	}
	sort.Strings(scopes)
	in.Scopes = scopes
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return IssuedIntegration{}, err
	}
	token := "zbi_" + base64.RawURLEncoding.EncodeToString(raw)
	record, err := s.Repository.Create(ctx, actor, in, IntegrationTokenHash(token), token[:12])
	if err != nil {
		return IssuedIntegration{}, err
	}
	return IssuedIntegration{Credential: record, Token: token}, nil
}
func (s Integrations) List(ctx context.Context, actor uint, offset, limit int) ([]IntegrationToken, error) {
	if actor == 0 {
		return nil, ErrPermission
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, ErrIntegrationInput
	}
	return s.Repository.List(ctx, actor, offset, limit)
}
func (s Integrations) Revoke(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrPermission
	}
	if id == 0 {
		return ErrIntegrationInput
	}
	return s.Repository.Revoke(ctx, actor, id)
}
