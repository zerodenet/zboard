package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/localworkers"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestProjectionPrincipalBacklogUsesOnlyIndexedThinCandidates(t *testing.T) {
	db := schemaFixture(t, datastore.DriverSQLite)
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Minute)
	oldest := now.Add(-time.Hour).Truncate(time.Millisecond)
	raw := meteringstore.PrincipalFlowScopeObservation{ScopeType: "subscription", ScopeID: 4, ActiveFlows: 3, NodeID: 2, CoreInstanceID: "original-core", EventID: "candidate", Source: "principal", ObservedAt: oldest, CreatedAt: oldest}
	if err := db.Create(&raw).Error; err != nil {
		t.Fatal(err)
	}
	pending := model.PrincipalStatisticsPending{ObservationID: uint64(raw.ID), ScopeType: raw.ScopeType, ScopeID: raw.ScopeID, ObservedAt: oldest, CreatedAt: oldest}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	var statements []string
	const callback = "test:thin_principal_capacity"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) { statements = append(statements, tx.Statement.SQL.String()) }); err != nil {
		t.Fatal(err)
	}
	found, at, err := projectionPrincipalStatisticsBacklog(t.Context(), db, now)
	if e := db.Callback().Query().Remove(callback); e != nil {
		t.Fatal(e)
	}
	if err != nil || !found || !at.Equal(oldest) {
		t.Fatal("pending source missing", found, at, err)
	}
	if len(statements) != 2 {
		t.Fatal("capacity query count was not fixed", statements)
	}
	for _, query := range statements {
		upper := strings.ToUpper(query)
		if strings.Contains(upper, "COUNT(") || strings.Contains(upper, "SELECT *") || !strings.Contains(upper, "LIMIT 1") {
			t.Fatal("history scan/unbounded candidate", query)
		}
	}
	if err := db.Delete(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PrincipalStatisticsState{}).Where("id = 1").Updates(map[string]any{"legacy_upper_id": raw.ID, "legacy_expected_rows": 1}).Error; err != nil {
		t.Fatal(err)
	}
	found, at, err = projectionPrincipalStatisticsBacklog(t.Context(), db, now)
	if err != nil || !found || !at.Equal(oldest) {
		t.Fatal("legacy backlog candidate missing", found, at, err)
	}
	if err := db.Delete(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if found, _, err := projectionPrincipalStatisticsBacklog(t.Context(), db, now); err == nil || found {
		t.Fatal("missing captured source became idle zero", found, err)
	}
	if err := db.Model(&model.PrincipalStatisticsState{}).Where("id = 1").Updates(map[string]any{"legacy_upper_id": 0, "legacy_expected_rows": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if found, at, err := projectionPrincipalStatisticsBacklog(t.Context(), db, now); err != nil || found || !at.IsZero() {
		t.Fatal("known empty queue misread", found, at, err)
	}
	pending.CreatedAt = now.Add(time.Hour)
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if found, _, err := projectionPrincipalStatisticsBacklog(t.Context(), db, now); err == nil || found {
		t.Fatal("future candidate became zero/healthy", found, err)
	}
	if err := db.Delete(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = 1").Delete(&model.PrincipalStatisticsState{}).Error; err != nil {
		t.Fatal(err)
	}
	if found, _, err := projectionPrincipalStatisticsBacklog(t.Context(), db, now); err == nil || found {
		t.Fatal("missing checkpoint became idle zero", found, err)
	}
}

type principalPressureResources struct{}

func (principalPressureResources) Sample(now time.Time) localworkers.Resources {
	return localworkers.Resources{At: now, Known: true}
}

func TestProjectionCapacityIncludesStatisticsQueueAndRetentionDurablePressure(t *testing.T) {
	db := schemaFixture(t, datastore.DriverSQLite)
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := platformstoreDefaults(db).Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	app := New(db, "")
	defer app.Close()
	app.StartWork()
	now := time.Now().UTC().Add(10 * time.Second)
	old := now.Add(-time.Minute)
	pending := model.PrincipalStatisticsPending{ObservationID: 99, ScopeType: "user", ScopeID: 7, ObservedAt: old, CreatedAt: old}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	options := &ProjectionSupervisorOptions{Policy: localworkers.DefaultPolicy(), Resources: principalPressureResources{}}
	o := app.projectionCapacityObservation(t.Context(), now, options)
	if !o.QueueKnown || !o.Pending || o.OldestAge < time.Minute || !o.ProjectionActivityKnown {
		t.Fatal("statistics backlog omitted", o)
	}
	if err := db.Delete(&pending).Error; err != nil {
		t.Fatal(err)
	}
	store := jobstore.New(db)
	run, err := store.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "retention-capacity", Handler: PrincipalScopeRetentionJobID, Resource: "core:" + PrincipalScopeRetentionJobID, Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.Claim(t.Context(), "independent-retention-child", []string{run.Handler}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	o = app.projectionCapacityObservation(t.Context(), now, options)
	if !o.QueueKnown || o.Pending || !o.ProjectionActivityKnown || o.ProjectionRuntime < 9*time.Second {
		t.Fatal("retention was scale trigger or missing durable pressure", o)
	}
	if err := store.Finish(t.Context(), claim, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	o = app.projectionCapacityObservation(context.Background(), now, options)
	if o.ProjectionActivityKnown {
		t.Fatal("unknown independent retention did not suppress expansion", o)
	}
}
