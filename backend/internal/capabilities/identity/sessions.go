package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrToken = errors.New("invalid token")
var ErrExpired = errors.New("token expired")

type SessionClaims struct {
	UserID  uint   `json:"uid"`
	Email   string `json:"e"`
	IsAdmin bool   `json:"a"`
	Expiry  int64  `json:"exp"`
}

// SessionTokens preserves the existing signed envelope. Transport adapters
// extract the token; identity owns signature, lifetime and current account scope.
type SessionTokens struct {
	Key []byte
	Now func() time.Time
}

func (s SessionTokens) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s SessionTokens) Sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.Key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
func (s SessionTokens) Issue(in SessionClaims) (string, int64, error) {
	if len(s.Key) == 0 || in.UserID == 0 || in.Email == "" {
		return "", 0, ErrToken
	}
	in.Expiry = s.now().Add(24 * time.Hour).Unix()
	payload, err := json.Marshal(in)
	if err != nil {
		return "", 0, err
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(s.Sign(payload)), in.Expiry, nil
}
func (s SessionTokens) Parse(raw string) (SessionClaims, error) {
	if len(s.Key) == 0 || len(raw) > 16384 {
		return SessionClaims{}, ErrToken
	}
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return SessionClaims{}, ErrToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return SessionClaims{}, ErrToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, s.Sign(payload)) {
		return SessionClaims{}, ErrToken
	}
	var in SessionClaims
	if json.Unmarshal(payload, &in) != nil || in.UserID == 0 || in.Email == "" || in.Expiry <= 0 {
		return SessionClaims{}, ErrToken
	}
	if in.Expiry <= s.now().Unix() {
		return SessionClaims{}, ErrExpired
	}
	return in, nil
}

type SessionAccounts interface {
	CurrentAccount(context.Context, uint) (PublicAccount, error)
}
type Sessions struct {
	Tokens   SessionTokens
	Accounts SessionAccounts
}

func (s Sessions) Authenticate(ctx context.Context, raw string) (SessionClaims, error) {
	claims, err := s.Tokens.Parse(raw)
	if err != nil {
		return SessionClaims{}, err
	}
	current, err := s.Accounts.CurrentAccount(ctx, claims.UserID)
	if err != nil {
		return SessionClaims{}, err
	}
	if current.ID != claims.UserID || current.Status != "active" {
		return SessionClaims{}, ErrUnavailable
	}
	claims.Email = current.Email
	claims.IsAdmin = current.IsAdmin
	return claims, nil
}
