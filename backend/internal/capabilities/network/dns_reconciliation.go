package network

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

var ErrDNSUnverified = errors.New("DNS outcome requires more evidence")
var ErrDNSChanged = errors.New("DNS reconciliation scope changed")

type DNSReconciliationScope struct {
	RunID                                             string
	Reviewer, OperationID, RecordID, AccountID        uint
	Revision, AccountRevision                         uint64
	ZoneID, RemoteID, Domain, RecordType, DesiredHash string
}
type DNSObservation struct{ ZoneID, RemoteID, Domain, RecordType, Hash string }
type DNSObserver interface {
	ObserveDNS(context.Context, DNSReconciliationScope) (DNSObservation, error)
}
type DNSReconciliationStore interface {
	LoadDNSReconciliation(context.Context, uint, string) (DNSReconciliationScope, error)
	LoadDNSObservationCredential(context.Context, DNSReconciliationScope) (string, error)
	CompleteDNSReconciliation(context.Context, DNSReconciliationScope, DNSObservation) error
}

func (s DNSReconciliation) ObservationCredential(ctx context.Context, scope DNSReconciliationScope) (string, error) {
	credential, err := s.Store.LoadDNSObservationCredential(ctx, scope)
	if err != nil || credential == "" {
		return "", ErrDNSChanged
	}
	return credential, nil
}

type DNSReconciliation struct {
	Store    DNSReconciliationStore
	Observer DNSObserver
}

func DNSRecordHash(recordType, domain, value string, ttl int, proxied bool) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%s\n%d\n%t", strings.ToUpper(recordType), strings.ToLower(domain), value, ttl, proxied)))
	return fmt.Sprintf("%x", sum[:])
}
func (s DNSReconciliation) Prepare(ctx context.Context, reviewer uint, runID string) (DNSReconciliationScope, error) {
	if reviewer == 0 || runID == "" || len(runID) > 36 {
		return DNSReconciliationScope{}, ErrDNSChanged
	}
	scope, err := s.Store.LoadDNSReconciliation(ctx, reviewer, runID)
	if err != nil {
		return DNSReconciliationScope{}, err
	}
	if scope.Reviewer != reviewer || scope.RunID != runID || scope.ZoneID == "" || scope.RemoteID == "" || scope.DesiredHash == "" {
		return DNSReconciliationScope{}, ErrDNSUnverified
	}
	return scope, nil
}
func (s DNSReconciliation) Reconcile(ctx context.Context, reviewer uint, runID string) error {
	scope, err := s.Prepare(ctx, reviewer, runID)
	if err != nil {
		return err
	}
	observation, err := s.Observer.ObserveDNS(ctx, scope)
	if err != nil {
		return err
	}
	if observation.ZoneID != scope.ZoneID || observation.RemoteID != scope.RemoteID || !strings.EqualFold(observation.Domain, scope.Domain) || !strings.EqualFold(observation.RecordType, scope.RecordType) || observation.Hash != scope.DesiredHash {
		return ErrDNSUnverified
	}
	return s.Store.CompleteDNSReconciliation(ctx, scope, observation)
}
