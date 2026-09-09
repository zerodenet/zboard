package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type externalAuthFlow struct {
	Provider     plugins.IdentitySnapshot
	Binding      string
	Nonce        string
	Verifier     string
	RedirectURI  string
	BindUserID   uint
	PasswordHash string
	Expires      time.Time
}
type externalAuthCompletion struct {
	Identity      *pluginv1.VerifiedIdentity
	PasswordSetup bool
	Provider      plugins.IdentitySnapshot
	UserID        uint
	IdentityID    string
	Linked        bool
	Expires       time.Time
}
type externalAuthState struct {
	mu      sync.Mutex
	flows   map[string]externalAuthFlow
	results map[string]externalAuthCompletion
}

func authRandom() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
func (s *externalAuthState) clean(now time.Time) {
	if s.flows == nil {
		s.flows = map[string]externalAuthFlow{}
		s.results = map[string]externalAuthCompletion{}
	}
	for k, v := range s.flows {
		if !now.Before(v.Expires) {
			delete(s.flows, k)
		}
	}
	for k, v := range s.results {
		if !now.Before(v.Expires) {
			delete(s.results, k)
		}
	}
}
func (s *externalAuthState) add(flow externalAuthFlow, previousBinding string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clean(time.Now())
	for key, prior := range s.flows {
		if previousBinding != "" && prior.Binding == previousBinding {
			delete(s.flows, key)
		}
	}
	if len(s.flows) >= 1024 {
		return "", errors.New("too many pending identity requests")
	}
	state, err := authRandom()
	if err != nil {
		return "", err
	}
	flow.Expires = time.Now().Add(5 * time.Minute)
	s.flows[state] = flow
	return state, nil
}
func (s *externalAuthState) take(state, binding string) (externalAuthFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clean(time.Now())
	flow, ok := s.flows[state]
	if !ok || len(binding) != 43 || subtle.ConstantTimeCompare([]byte(flow.Binding), []byte(binding)) != 1 {
		return externalAuthFlow{}, errors.New("invalid or expired login state")
	}
	delete(s.flows, state)
	return flow, nil
}
func (s *externalAuthState) complete(result externalAuthCompletion) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clean(time.Now())
	if len(s.results) >= 1024 {
		return "", errors.New("too many pending login results")
	}
	ticket, err := authRandom()
	if err != nil {
		return "", err
	}
	if result.Expires.IsZero() {
		ttl := time.Minute
		if result.Identity != nil || result.PasswordSetup {
			ttl = 5 * time.Minute
		}
		result.Expires = time.Now().Add(ttl)
	}
	s.results[ticket] = result
	return ticket, nil
}
func (s *externalAuthState) finish(ticket string) (externalAuthCompletion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clean(time.Now())
	result, ok := s.results[ticket]
	delete(s.results, ticket)
	if !ok {
		return result, errors.New("invalid or expired login result")
	}
	return result, nil
}
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *externalAuthState) peek(ticket string) (externalAuthCompletion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clean(time.Now())
	result, ok := s.results[ticket]
	if !ok {
		return result, errors.New("invalid or expired authentication result")
	}
	return result, nil
}
