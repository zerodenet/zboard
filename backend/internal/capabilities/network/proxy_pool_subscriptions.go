package network

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	ProxyPoolSubscriptionDefaultAgent    = "Clash.Meta"
	ProxyPoolSubscriptionFinalizeTimeout = 5 * time.Second
	ProxyPoolSubscriptionFailureDelay    = 5 * time.Minute
)

var ErrProxyPoolSubscriptionNotConfigured = errors.New("请先配置订阅地址")

type ProxyPoolSubscriptionActor struct {
	AccountID uint
	System    bool
}

type ProxyPoolSubscriptionSnapshot struct {
	Pool                      ProxyPoolRecord
	SubscriptionURLCiphertext string
	SubscriptionFormat        string
	SubscriptionUserAgent     string
}

type ProxyPoolSubscriptionSettings struct {
	URL                 string
	Format              string
	UserAgent           string
	SyncIntervalSeconds int
}

type PreparedProxyPoolSubscription struct {
	Snapshot ProxyPoolSubscriptionSnapshot
	Settings ProxyPoolSubscriptionSettings
}

type ProxyPoolSubscriptionDueClaim struct {
	ID       uint
	Revision uint64
}

type ProxyPoolSubscriptionRepository interface {
	LoadProxyPoolSubscription(context.Context, ProxyPoolSubscriptionActor, uint) (ProxyPoolSubscriptionSnapshot, error)
	CommitProxyPoolSubscription(context.Context, ProxyPoolSubscriptionActor, ProxyPoolSubscriptionSnapshot, string, int, ProxyPoolConfigurationFacts, time.Time) (ProxyPoolRecord, error)
	RecordProxyPoolSubscriptionFailure(context.Context, ProxyPoolSubscriptionActor, ProxyPoolSubscriptionSnapshot, string, time.Time) error
	ClaimDueProxyPoolSubscriptions(context.Context, time.Time, time.Time, int) ([]ProxyPoolSubscriptionDueClaim, error)
}

type ProxyPoolSubscriptions struct {
	Repository ProxyPoolSubscriptionRepository
	Cipher     ProviderCredentialCipher
	Inspector  ProxyPoolConfigurationInspector
	Now        func() time.Time
}

func (s ProxyPoolSubscriptions) Prepare(ctx context.Context, actor ProxyPoolSubscriptionActor, id uint, expected *uint64) (PreparedProxyPoolSubscription, error) {
	if s.Repository == nil || s.Cipher == nil || s.Inspector == nil || id == 0 || !validProxyPoolSubscriptionActor(actor) {
		return PreparedProxyPoolSubscription{}, ErrProxyPoolMutationUnavailable
	}
	snapshot, err := s.Repository.LoadProxyPoolSubscription(ctx, actor, id)
	if err != nil {
		return PreparedProxyPoolSubscription{}, err
	}
	if expected != nil && snapshot.Pool.Revision != *expected {
		return PreparedProxyPoolSubscription{}, ErrProxyPoolConflict
	}
	if snapshot.SubscriptionURLCiphertext == "" {
		return PreparedProxyPoolSubscription{}, ErrProxyPoolSubscriptionNotConfigured
	}
	url, err := s.Cipher.Decrypt(snapshot.SubscriptionURLCiphertext)
	if err != nil {
		return PreparedProxyPoolSubscription{}, errors.New("代理池订阅地址无法解密")
	}
	settings := ProxyPoolSubscriptionSettings{
		URL: strings.TrimSpace(url), Format: strings.TrimSpace(snapshot.SubscriptionFormat),
		UserAgent: strings.TrimSpace(snapshot.SubscriptionUserAgent), SyncIntervalSeconds: snapshot.Pool.SyncIntervalSeconds,
	}
	if settings.Format == "" {
		settings.Format = "auto"
	}
	if settings.UserAgent == "" {
		settings.UserAgent = ProxyPoolSubscriptionDefaultAgent
	}
	if settings.SyncIntervalSeconds <= 0 {
		settings.SyncIntervalSeconds = ProxyPoolDefaultSyncInterval
	}
	return PreparedProxyPoolSubscription{Snapshot: snapshot, Settings: settings}, nil
}

func (s ProxyPoolSubscriptions) Commit(ctx context.Context, actor ProxyPoolSubscriptionActor, prepared PreparedProxyPoolSubscription, raw string, nodeCount int) (ProxyPoolRecord, error) {
	if s.Repository == nil || s.Cipher == nil || s.Inspector == nil || !validProxyPoolSubscriptionActor(actor) || prepared.Snapshot.Pool.ID == 0 {
		return ProxyPoolRecord{}, ErrProxyPoolMutationUnavailable
	}
	facts, err := s.Inspector.InspectProxyPoolConfiguration(ctx, raw, false)
	if err != nil {
		return ProxyPoolRecord{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(raw)
	if err != nil {
		return ProxyPoolRecord{}, err
	}
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ProxyPoolSubscriptionFinalizeTimeout)
	defer cancel()
	return s.Repository.CommitProxyPoolSubscription(finalizeCtx, actor, prepared.Snapshot, ciphertext, nodeCount, facts, s.now())
}

func (s ProxyPoolSubscriptions) RecordFailure(ctx context.Context, actor ProxyPoolSubscriptionActor, prepared PreparedProxyPoolSubscription, cause error) error {
	if s.Repository == nil || prepared.Snapshot.Pool.ID == 0 || cause == nil || !validProxyPoolSubscriptionActor(actor) {
		return ErrProxyPoolMutationUnavailable
	}
	message := truncateProxyPoolSubscriptionError(cause.Error(), 1000)
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ProxyPoolSubscriptionFinalizeTimeout)
	defer cancel()
	return s.Repository.RecordProxyPoolSubscriptionFailure(finalizeCtx, actor, prepared.Snapshot, message, s.now().Add(ProxyPoolSubscriptionFailureDelay))
}

func (s ProxyPoolSubscriptions) ClaimDue(ctx context.Context, now time.Time, limit int) ([]ProxyPoolSubscriptionDueClaim, error) {
	if s.Repository == nil || limit <= 0 || limit > 100 {
		return nil, ErrProxyPoolMutationUnavailable
	}
	now = now.UTC()
	return s.Repository.ClaimDueProxyPoolSubscriptions(ctx, now, now.Add(ProxyPoolSubscriptionFailureDelay), limit)
}

func (s ProxyPoolSubscriptions) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func validProxyPoolSubscriptionActor(actor ProxyPoolSubscriptionActor) bool {
	return (actor.System && actor.AccountID == 0) || (!actor.System && actor.AccountID != 0)
}

func truncateProxyPoolSubscriptionError(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
