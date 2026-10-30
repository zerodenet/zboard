package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"sort"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

type BufferedEventProjection struct {
	Sources  []network.ObservationSource
	Nodes    []network.NodeObservation
	Flows    []metering.FlowSample
	Coverage []metering.NodeCoverageEvent
}

type BufferedEventProjectionResult struct {
	Exhausted   []metering.FlowAccountingResult
	CoverageErr error
	ClosedFlows int
}

// ProjectBufferedEvents atomically settles financial facts and appends immutable
// auxiliary intents. A later observation failure cannot roll back settlement;
// inability to retain its source still rolls back, leaving the spool replayable.
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
		var node model.Node
		if err := s.Identity.db.WithContext(ctx).Select("id,lifecycle_status").First(&node, id).Error; err != nil {
			return result, err
		}
		if network.NodeManagementClosed(node.LifecycleStatus) {
			continue
		}
		if err := (credentialstore.PrincipalIndex{DB: s.Identity.db, Cipher: cipher}).Prepare(ctx, id); err != nil {
			return result, err
		}
	}
	var err error
	if len(input.Flows) > 0 || len(input.Nodes) > 0 || len(input.Sources) > 0 {
		err = s.nativeAuthorityTransaction(ctx, input, func(tx *gorm.DB) error {
			// Retain exact parent SHARE NOWAIT locks through settlement and source append.
			// Terminal source bodies are still enqueued, held by the existing tombstone.
			flows, closed, err := managedFlowInputs(tx, input.Flows)
			if err != nil {
				return err
			}
			result.ClosedFlows = closed
			result.Exhausted, err = ApplyFlowBatch(tx, flows, cipher)
			if err != nil {
				return err
			}
			sources := make([]network.ObservationSource, 0, len(input.Sources)+len(input.Nodes))
			sources = append(sources, input.Sources...)
			for _, observation := range input.Nodes {
				sources = append(sources, network.ObservationSource{Observation: observation, Kind: "legacy_projection"})
			}
			if _, err := observationstore.EnqueueBatch(tx, sources); err != nil {
				return err
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
