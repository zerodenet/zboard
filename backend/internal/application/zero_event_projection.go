package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"sort"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

type BufferedEventProjection struct {
	Nodes    []network.NodeObservation
	Flows    []metering.FlowSample
	Coverage []metering.NodeCoverageEvent
}

type BufferedEventProjectionResult struct {
	Exhausted   []metering.FlowAccountingResult
	CoverageErr error
}

// ProjectBufferedEvents keeps node observation and billable flow settlement in
// one authority transaction. Coverage is auxiliary observability state and is
// intentionally committed separately so it remains fail-open.
func (s *Services) ProjectBufferedEvents(ctx context.Context, input BufferedEventProjection, cipher metering.CredentialDecryptor) (BufferedEventProjectionResult, error) {
	result := BufferedEventProjectionResult{}
	// Commit legacy attribution projection progress independently. Rolling back
	// a later sample must not force a large node to restart its backfill forever.
	nodes := make(map[uint]bool)
	for _, flow := range input.Flows {
		nodes[flow.NodeID] = true
	}
	ids := make([]uint, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := (credentialstore.PrincipalIndex{DB: s.Identity.db, Cipher: cipher}).Prepare(ctx, id); err != nil {
			return result, err
		}
	}
	var err error
	if len(input.Flows) > 0 || len(input.Nodes) > 0 {
		err = s.Identity.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Account first: waiting for an entitlement must not hold a node stats
			// write lock. Both projections still commit or roll back together.
			var err error
			result.Exhausted, err = ApplyFlowBatch(tx, input.Flows, cipher)
			if err != nil {
				return err
			}
			for _, observation := range input.Nodes {
				if err := ProjectNodeObservation(tx, observation); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if err != nil {
		return result, err
	}
	if len(input.Coverage) > 0 {
		result.CoverageErr = s.Identity.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return ProjectFairUseCoverage(tx, input.Coverage)
		})
	}
	return result, nil
}
