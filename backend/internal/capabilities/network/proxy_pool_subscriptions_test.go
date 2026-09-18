package network

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type proxyPoolSubscriptionRepositoryStub struct {
	snapshot ProxyPoolSubscriptionSnapshot
	actor    ProxyPoolSubscriptionActor
	cipher   string
	facts    ProxyPoolConfigurationFacts
	failure  string
	next     time.Time
}

func (s *proxyPoolSubscriptionRepositoryStub) LoadProxyPoolSubscription(_ context.Context, actor ProxyPoolSubscriptionActor, id uint) (ProxyPoolSubscriptionSnapshot, error) {
	if id != s.snapshot.Pool.ID {
		return ProxyPoolSubscriptionSnapshot{}, ErrProxyPoolNotFound
	}
	s.actor = actor
	return s.snapshot, nil
}

func (s *proxyPoolSubscriptionRepositoryStub) CommitProxyPoolSubscription(_ context.Context, actor ProxyPoolSubscriptionActor, snapshot ProxyPoolSubscriptionSnapshot, ciphertext string, _ int, facts ProxyPoolConfigurationFacts, _ time.Time) (ProxyPoolRecord, error) {
	s.actor, s.snapshot, s.cipher, s.facts = actor, snapshot, ciphertext, facts
	return ProxyPoolRecord{ID: snapshot.Pool.ID, Revision: snapshot.Pool.Revision + 1}, nil
}

func (s *proxyPoolSubscriptionRepositoryStub) RecordProxyPoolSubscriptionFailure(_ context.Context, actor ProxyPoolSubscriptionActor, snapshot ProxyPoolSubscriptionSnapshot, message string, next time.Time) error {
	s.actor, s.snapshot, s.failure, s.next = actor, snapshot, message, next
	return nil
}

func (*proxyPoolSubscriptionRepositoryStub) ClaimDueProxyPoolSubscriptions(context.Context, time.Time, time.Time, int) ([]ProxyPoolSubscriptionDueClaim, error) {
	return []ProxyPoolSubscriptionDueClaim{{ID: 3, Revision: 4}}, nil
}

func TestProxyPoolSubscriptionsPrepareCommitAndRecordFailure(t *testing.T) {
	now := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	repository := &proxyPoolSubscriptionRepositoryStub{snapshot: ProxyPoolSubscriptionSnapshot{
		Pool: ProxyPoolRecord{ID: 3, Revision: 4}, SubscriptionURLCiphertext: "enc:https://example.test/sub",
	}}
	service := ProxyPoolSubscriptions{
		Repository: repository, Cipher: proxyPoolMutationCipher{}, Now: func() time.Time { return now },
		Inspector: proxyPoolMutationInspectorFunc(func(_ context.Context, raw string, validateNative bool) (ProxyPoolConfigurationFacts, error) {
			if raw != "replacement" || validateNative {
				t.Fatalf("raw=%q validate_native=%v", raw, validateNative)
			}
			return ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
		}),
	}
	expected := uint64(4)
	prepared, err := service.Prepare(context.Background(), ProxyPoolSubscriptionActor{AccountID: 7}, 3, &expected)
	if err != nil || prepared.Settings.URL != "https://example.test/sub" || prepared.Settings.Format != "auto" || prepared.Settings.UserAgent != ProxyPoolSubscriptionDefaultAgent || prepared.Settings.SyncIntervalSeconds != ProxyPoolDefaultSyncInterval {
		t.Fatalf("prepared=%+v error=%v", prepared, err)
	}
	updated, err := service.Commit(context.Background(), ProxyPoolSubscriptionActor{AccountID: 7}, prepared, "replacement", 2)
	if err != nil || updated.Revision != 5 || repository.cipher != "enc:replacement" || !repository.facts.SupportsDatagram {
		t.Fatalf("updated=%+v repository=%+v error=%v", updated, repository, err)
	}
	long := strings.Repeat("界", 1001)
	if err := service.RecordFailure(context.Background(), ProxyPoolSubscriptionActor{AccountID: 7}, prepared, errors.New(long)); err != nil {
		t.Fatal(err)
	}
	if len([]rune(repository.failure)) != 1000 || !repository.next.Equal(now.Add(ProxyPoolSubscriptionFailureDelay)) {
		t.Fatalf("failure_runes=%d next=%v", len([]rune(repository.failure)), repository.next)
	}
	stale := uint64(3)
	if _, err := service.Prepare(context.Background(), ProxyPoolSubscriptionActor{AccountID: 7}, 3, &stale); !errors.Is(err, ErrProxyPoolConflict) {
		t.Fatalf("stale error=%v", err)
	}
}
