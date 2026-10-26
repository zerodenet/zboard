package application

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// RunProjectionWorker uses the same durable jobs and bounded projections as the
// complete server. It never prepares schema, starts HTTP/plugins, or opens the
// single-owner event spool. A canceled caller requests process-local drain;
// Close cancels remaining local execution only after the drain budget expires.
// Other processes' registrations, pending intents and leases remain intact.
func RunProjectionWorker(ctx context.Context, db *gorm.DB, drainTimeout time.Duration) error {
	if ctx == nil || drainTimeout < time.Second || drainTimeout > 5*time.Minute {
		return errors.New("projection worker requires a context and a drain timeout between one second and five minutes")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidatePreparedProjectionDatabase(ctx, db); err != nil {
		return err
	}
	app := New(db, "")
	defer app.Close()
	for _, job := range []PeriodicJob{app.UsageProjectionJob(), app.NodeObservationJob()} {
		if err := app.RegisterPeriodicJob(job); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	app.StartWork()
	<-ctx.Done()
	drain, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	return app.Jobs.Drain(drain)
}
