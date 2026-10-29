package application

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/localworkers"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type ProjectionResourceSampler interface {
	Sample(time.Time) localworkers.Resources
}

type ProjectionSupervisorOptions struct {
	Policy       localworkers.Policy
	Interval     time.Duration
	DrainTimeout time.Duration
	LockPath     string
	Launcher     localworkers.Launcher
	Resources    ProjectionResourceSampler
	Report       func(localworkers.Status)
}

// StartProjectionSupervisor is called only after the complete server opens its
// work gate. Default nil/zero policy adds no file, process, query or goroutine.
// A supervisor is capacity assistance; its ownership/configuration failure does
// not alter any durable job, business authorization or the single-process core.
func (s *Services) StartProjectionSupervisor(options *ProjectionSupervisorOptions) (func() error, error) {
	if options == nil || options.Policy.MaxWorkers == 0 {
		return nil, nil
	}
	configured := *options
	options = &configured
	if !s.isReady() || options.LockPath == "" || options.Resources == nil {
		return nil, errors.New("local projection supervisor is not configured or startup is incomplete")
	}
	if s.Identity.db.Dialector.Name() == "sqlite" && options.Policy.MaxWorkers > 1 {
		return nil, errors.New("SQLite local projection supervision supports at most one additional process")
	}
	controller := &localworkers.Supervisor{Policy: options.Policy, Interval: options.Interval, DrainTimeout: options.DrainTimeout, Launcher: options.Launcher}
	controller.Observe = func(ctx context.Context, now time.Time) localworkers.Observation {
		return s.projectionCapacityObservation(ctx, now, options)
	}
	lastReason, lastReport := "", time.Time{}
	controller.Report = func(status localworkers.Status) {
		if options.Report != nil {
			options.Report(status)
		}
		if status.Reason != lastReason || lastReport.IsZero() || status.At.Sub(lastReport) >= time.Minute {
			log.Printf("local projection supervisor: running=%d draining=%d desired=%d reason=%s administrator_confirmed_benefit=%t", status.Running, status.Draining, status.Desired, status.Reason, options.Policy.AdministratorConfirmedBenefit)
			lastReason, lastReport = status.Reason, status.At
		}
	}
	if err := controller.Validate(); err != nil {
		return nil, err
	}
	ownership, err := localworkers.AcquireLock(options.LockPath)
	if err != nil {
		return nil, err
	}
	if launcher, ok := options.Launcher.(*localworkers.CommandLauncher); ok {
		launcher.Ownership = ownership
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		err := controller.Run(ctx)
		done <- errors.Join(err, ownership.Close())
		close(done)
	}()
	var once sync.Once
	var result error
	return func() error {
		once.Do(func() {
			cancel()
			// Run owns the launch/reap goroutine and bounded process-local drain.
			// No global cancellation/revocation is used to force a scale-down.
			// A stalled local filesystem observation must not keep core shutdown
			// waiting forever. Timeout is explicitly unconfirmed: ownership stays
			// with Run/the inherited child descriptors until they actually exit.
			deadline := time.NewTimer(options.DrainTimeout + 4*time.Second)
			defer deadline.Stop()
			select {
			case result = <-done:
			case <-deadline.C:
				result = errors.New("local projection supervisor shutdown not confirmed; ownership retained")
			}
		})
		return result
	}, nil
}

func (s *Services) projectionCapacityObservation(ctx context.Context, now time.Time, options *ProjectionSupervisorOptions) localworkers.Observation {
	o := localworkers.Observation{At: now, Resources: options.Resources.Sample(now)}
	if !s.isReady() {
		o.Paused = true
		return o
	}
	maintenance, err := s.MaintenanceState(ctx, false)
	if err != nil {
		o.Paused, o.DatabaseFailed = true, true
		return o
	}
	o.Paused = maintenance.Enabled
	if o.Paused {
		return o
	}
	pressure := s.Jobs.DatabasePressure(now, options.Policy.Freshness)
	o.DatabaseAt, o.DatabaseWrites, o.DatabaseLatency, o.DatabaseFailed = pressure.ObservedAt, pressure.Writes, pressure.MaxLatency, pressure.Failed
	o.StorageLatency, o.StorageUnknown, o.StorageFailed = pressure.MaxStorageLatency, pressure.StorageUnknown, pressure.StorageFailed
	activity := jobstore.InspectProjectionActivity(ctx, s.Identity.db, []string{UsageProjectionJobID, NodeObservationJobID, PrincipalStatisticsJobID, PrincipalScopeRetentionJobID, FlowStartStatisticsJobID}, now)
	o.ProjectionActivityKnown, o.ProjectionRuntime = activity.Known, activity.Longest
	o.DatabaseCapacityKnown, o.DatabaseCapacityAvailable = jobstore.LocalWorkerConnectionCapacity(ctx, s.Identity.db)
	var usage struct{ CreatedAt time.Time }
	query := s.Identity.db.WithContext(ctx).Model(&model.MeteringUsageProjectionPending{}).Select("created_at").Order("traffic_record_id").Limit(1).Find(&usage)
	if query.Error != nil {
		o.DatabaseFailed = true
		return o
	}
	usageFound := query.RowsAffected != 0
	var observation struct{ CreatedAt time.Time }
	query = s.Identity.db.WithContext(ctx).Model(&model.NodeObservationIntent{}).Select("created_at").Where("(status = ? AND available_at <= ?) OR (status = ? AND lease_until <= ?)", "pending", now, "claimed", now).Order("available_at,id").Limit(1).Find(&observation)
	if query.Error != nil {
		o.DatabaseFailed = true
		return o
	}
	observationFound := query.RowsAffected != 0
	// Inspect only indexed pending/checkpoint candidates. Retention is included
	// in durable in-flight pressure above, but is not an independent scale-up
	// trigger: its single checkpoint does not prove multi-process benefit.
	statisticsFound, statisticsAt, err := projectionPrincipalStatisticsBacklog(ctx, s.Identity.db, now)
	if err != nil {
		o.DatabaseFailed = true
		return o
	}
	flowFound, flowAt, err := projectionFlowStartStatisticsBacklog(ctx, s.Identity.db, now)
	if err != nil {
		o.DatabaseFailed = true
		return o
	}
	o.QueueKnown = true
	for _, candidate := range []struct {
		found bool
		at    time.Time
	}{{usageFound, usage.CreatedAt}, {observationFound, observation.CreatedAt}, {statisticsFound, statisticsAt}, {flowFound, flowAt}} {
		if !candidate.found {
			continue
		}
		if candidate.at.IsZero() || candidate.at.After(now) {
			o.QueueKnown = false
			return o
		}
		o.Pending = true
		o.OldestAge = max(o.OldestAge, now.Sub(candidate.at))
	}
	return o
}

// projectionPrincipalStatisticsBacklog reads at most one state, one next legacy
// primary-key candidate and one pending primary-key candidate. It never counts
// history, loads variable source bodies, or interprets missing coverage as zero.
func projectionPrincipalStatisticsBacklog(ctx context.Context, db *gorm.DB, now time.Time) (bool, time.Time, error) {
	var state struct {
		ID                                                                                             uint64
		LegacyUpperID, LegacyAfterID, LegacyExpectedRows, LegacyProjectedRows, ProjectedHead, Revision uint64
		CreatedAt, UpdatedAt                                                                           time.Time
	}
	q := db.WithContext(ctx).Table("principal_statistics_state").
		Select("id,legacy_upper_id,legacy_after_id,legacy_expected_rows,legacy_projected_rows,projected_head,revision,created_at,updated_at").Where("id = ?", 1).Limit(1).Find(&state)
	if q.Error != nil {
		return false, time.Time{}, q.Error
	}
	if q.RowsAffected != 1 || state.ID != 1 || state.Revision == 0 || state.LegacyAfterID > state.LegacyUpperID || state.LegacyProjectedRows > state.LegacyExpectedRows || state.ProjectedHead < state.LegacyAfterID || (state.LegacyUpperID == 0) != (state.LegacyExpectedRows == 0) || state.CreatedAt.IsZero() || state.UpdatedAt.Before(state.CreatedAt) || state.UpdatedAt.After(now) || (state.LegacyAfterID == state.LegacyUpperID && state.LegacyProjectedRows != state.LegacyExpectedRows) {
		return false, time.Time{}, errors.New("principal statistics capacity coverage is unavailable")
	}
	found, oldest := false, time.Time{}
	take := func(at time.Time) error {
		if at.IsZero() || at.After(now) {
			return errors.New("principal statistics queue time is unavailable")
		}
		if !found || at.Before(oldest) {
			oldest = at
		}
		found = true
		return nil
	}
	if state.LegacyAfterID < state.LegacyUpperID {
		if state.LegacyProjectedRows >= state.LegacyExpectedRows {
			return false, time.Time{}, errors.New("principal statistics legacy coverage is inconsistent")
		}
		var legacy struct{ CreatedAt time.Time }
		q = db.WithContext(ctx).Table("principal_flow_scope_observations").Select("created_at").Where("id > ? AND id <= ?", state.LegacyAfterID, state.LegacyUpperID).Order("id").Limit(1).Find(&legacy)
		if q.Error != nil {
			return false, time.Time{}, q.Error
		}
		if q.RowsAffected != 1 {
			return false, time.Time{}, errors.New("principal statistics legacy source candidate is missing")
		}
		if err := take(legacy.CreatedAt); err != nil {
			return false, time.Time{}, err
		}
	}
	var pending struct{ CreatedAt time.Time }
	q = db.WithContext(ctx).Table("principal_statistics_pending").Select("created_at").Order("observation_id").Limit(1).Find(&pending)
	if q.Error != nil {
		return false, time.Time{}, q.Error
	}
	if q.RowsAffected != 0 {
		if err := take(pending.CreatedAt); err != nil {
			return false, time.Time{}, err
		}
	}
	return found, oldest, nil
}

// Flow statistics keeps the original raw TTL. The registration bit, rather than
// a historical MAX or full-table COUNT, discovers unprojected imports even when
// an empty copied allocator reuses IDs below the completed legacy checkpoint.
func projectionFlowStartStatisticsBacklog(ctx context.Context, db *gorm.DB, now time.Time) (bool, time.Time, error) {
	var state model.FlowStartStatisticsState
	q := db.WithContext(ctx).Select("id,legacy_upper_id,legacy_after_id,legacy_expected_rows,legacy_projected_rows,projected_head,revision,created_at,updated_at").Where("id=?", 1).Limit(1).Find(&state)
	if q.Error != nil {
		return false, time.Time{}, q.Error
	}
	if q.RowsAffected != 1 || state.ID != 1 || state.Revision == 0 || state.LegacyAfterID > state.LegacyUpperID || state.LegacyProjectedRows > state.LegacyExpectedRows || state.ProjectedHead < state.LegacyAfterID || (state.LegacyUpperID == 0) != (state.LegacyExpectedRows == 0) || (state.LegacyAfterID == state.LegacyUpperID && state.LegacyProjectedRows != state.LegacyExpectedRows) || state.CreatedAt.IsZero() || state.UpdatedAt.Before(state.CreatedAt) || state.UpdatedAt.After(now) {
		return false, time.Time{}, errors.New("flow-start statistics capacity coverage unavailable")
	}
	var oldest time.Time
	found := false
	for _, source := range []struct{ table, where, order string }{{"subscription_flow_start_events", "statistics_registered = ?", "id"}, {"flow_start_statistics_pending", "", "source_id"}} {
		var row struct{ CreatedAt time.Time }
		query := db.WithContext(ctx).Table(source.table).Select("created_at").Order(source.order).Limit(1)
		if source.where != "" {
			query = query.Where(source.where, false)
		}
		q := query.Find(&row)
		if q.Error != nil {
			return false, time.Time{}, q.Error
		}
		if q.RowsAffected == 0 {
			continue
		}
		if row.CreatedAt.IsZero() || row.CreatedAt.After(now) {
			return false, time.Time{}, errors.New("flow-start statistics queue time unavailable")
		}
		if !found || row.CreatedAt.Before(oldest) {
			oldest = row.CreatedAt
		}
		found = true
	}
	return found, oldest, nil
}
