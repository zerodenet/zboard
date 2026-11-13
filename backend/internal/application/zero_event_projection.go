package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
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

// ProjectBufferedEvents acknowledges the source only after every partition has
// committed. MySQL commits bounded contiguous quota partitions independently;
// any failure leaves the entire spool batch unacknowledged. Committed partitions
// replay through exact durable receipts, so neither a crash nor partial progress
// loses or repeats a charge. Each partition retains its observation atomically.
// SQLite preserves its existing single-writer transaction cohort.
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
	if err := meteringstore.EnsureIdentityFences(s.Identity.db.WithContext(ctx), ids); err != nil {
		return result, err
	}
	partitions := []meteringstore.FlowPartition{{Flows: input.Flows}}
	if s.Identity.db.Dialector.Name() == "mysql" {
		merged := false
		for _, flow := range input.Flows {
			merged = merged || len(flow.Checkpoints) > 0
		}
		if merged {
			if err := s.Identity.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return meteringstore.ValidateFlowWindows(tx, input.Flows) }); err != nil {
				return result, err
			}
		}
		var err error
		partitions, err = meteringstore.PlanFlowPartitions(ctx, s.Identity.db, cipher, input.Flows)
		if err != nil {
			return result, err
		}
	}
	projectedNodes := map[uint]bool{}
	for _, part := range partitions {
		nodeIDs := map[uint]bool{}
		for _, flow := range part.Flows {
			nodeIDs[flow.NodeID] = true
			projectedNodes[flow.NodeID] = true
		}
		scoped := BufferedEventProjection{Flows: part.Flows}
		for _, source := range input.Sources {
			if nodeIDs[source.Observation.NodeID] {
				scoped.Sources = append(scoped.Sources, source)
			}
		}
		for _, node := range input.Nodes {
			if nodeIDs[node.NodeID] {
				scoped.Nodes = append(scoped.Nodes, node)
			}
		}
		// SQLite also permits pure-observation work in its one original transaction.
		if s.Identity.db.Dialector.Name() != "mysql" {
			scoped.Sources = input.Sources
			scoped.Nodes = input.Nodes
		}
		var exhausted []metering.FlowAccountingResult
		var closed int
		err := s.nativeAuthorityTransaction(ctx, scoped, func(tx *gorm.DB) error {
			flows, n, e := managedFlowInputs(tx, scoped.Flows)
			if e != nil {
				return e
			}
			closed = n
			exhausted, e = (meteringstore.BatchAccounting{Cipher: cipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications, Quota: part.Quota}).Apply(tx, flows)
			if e != nil {
				return e
			}
			return enqueueBufferedObservations(tx, scoped)
		})
		if err != nil {
			return result, err
		}
		result.ClosedFlows += closed
		result.Exhausted = append(result.Exhausted, exhausted...)
		if s.Identity.db.Dialector.Name() == "mysql" {
			meteringstore.TouchCredentialActivity(ctx, s.Identity.db, part.CredentialIDs)
		}
	}
	if s.Identity.db.Dialector.Name() == "mysql" {
		rest := BufferedEventProjection{}
		for _, source := range input.Sources {
			if !projectedNodes[source.Observation.NodeID] {
				rest.Sources = append(rest.Sources, source)
			}
		}
		for _, node := range input.Nodes {
			if !projectedNodes[node.NodeID] {
				rest.Nodes = append(rest.Nodes, node)
			}
		}
		if len(rest.Sources) > 0 || len(rest.Nodes) > 0 {
			if err := s.nativeAuthorityTransaction(ctx, rest, func(tx *gorm.DB) error { return enqueueBufferedObservations(tx, rest) }); err != nil {
				return result, err
			}
		}
	}
	if len(input.Coverage) > 0 {
		result.CoverageErr = s.Identity.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return ProjectFairUseCoverage(tx, input.Coverage)
		})
	}
	return result, nil
}

func enqueueBufferedObservations(tx *gorm.DB, input BufferedEventProjection) error {
	sources := make([]network.ObservationSource, 0, len(input.Sources)+len(input.Nodes))
	sources = append(sources, input.Sources...)
	for _, o := range input.Nodes {
		sources = append(sources, network.ObservationSource{Observation: o, Kind: "legacy_projection"})
	}
	return observationstore.EnqueueLatest(tx, sources)
}
