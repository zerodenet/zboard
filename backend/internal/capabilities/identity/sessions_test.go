package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type sessionAccountStub struct{ Account PublicAccount }

func (s sessionAccountStub) CurrentAccount(context.Context, uint) (PublicAccount, error) {
	return s.Account, nil
}
func TestSessionsPreserveEnvelopeAndUseCurrentAuthority(t *testing.T) {
	now := time.Unix(2000000000, 0)
	codec := SessionTokens{Key: []byte("local-test-key"), Now: func() time.Time { return now }}
	token, expiry, err := codec.Issue(SessionClaims{UserID: 7, Email: "old@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if expiry != now.Add(24*time.Hour).Unix() {
		t.Fatal(expiry)
	}
	service := Sessions{Tokens: codec, Accounts: sessionAccountStub{PublicAccount{ID: 7, Email: "current@example.test", Status: "active", IsAdmin: false}}}
	claims, err := service.Authenticate(context.Background(), token)
	if err != nil || claims.Email != "current@example.test" || claims.IsAdmin {
		t.Fatal(claims, err)
	}
	for _, account := range []PublicAccount{{ID: 7, Status: "suspended"}, {ID: 8, Status: "active"}} {
		service.Accounts = sessionAccountStub{account}
		if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrUnavailable) {
			t.Fatal(account, err)
		}
	}
	now = now.Add(24 * time.Hour)
	if _, err := codec.Parse(token); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
}
func TestSessionsRejectTamperingAndUnboundedSignedClaims(t *testing.T) {
	now := time.Unix(2000000000, 0)
	codec := SessionTokens{Key: []byte("test"), Now: func() time.Time { return now }}
	for _, claims := range []SessionClaims{{UserID: 1, Email: "a@example.test"}, {UserID: 0, Email: "a@example.test", Expiry: now.Add(time.Hour).Unix()}} {
		data, _ := json.Marshal(claims)
		token := base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(codec.Sign(data))
		if _, err := codec.Parse(token); !errors.Is(err, ErrToken) {
			t.Fatal(err)
		}
	}
	token, _, err := codec.Issue(SessionClaims{UserID: 1, Email: "a@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	other := SessionTokens{Key: []byte("another"), Now: codec.Now}
	if _, err := other.Parse(token); !errors.Is(err, ErrToken) {
		t.Fatal(err)
	}
	if _, _, err := (SessionTokens{}).Issue(SessionClaims{UserID: 1, Email: "a@example.test"}); !errors.Is(err, ErrToken) {
		t.Fatal(err)
	}
}
