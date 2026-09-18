package network

import (
	"context"
	"errors"
	"time"
)

type OperationKind string

const CertificateOperation OperationKind = "certificate_operation"
const DNSOperation OperationKind = "dns_operation"
const HistoryBatchSize = 1000

type OperationHistoryStore interface {
	PruneOperations(context.Context, OperationKind, time.Time, int) (int64, error)
}
type OperationHistory struct{ Store OperationHistoryStore }

func (s OperationHistory) Prune(ctx context.Context, kind OperationKind, before time.Time) (int64, error) {
	if (kind != CertificateOperation && kind != DNSOperation) || before.IsZero() {
		return 0, errors.New("invalid operation retention request")
	}
	return s.Store.PruneOperations(ctx, kind, before, HistoryBatchSize)
}
