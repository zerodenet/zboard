package server

import (
	"fmt"
	"log"
	"time"
)

// Stage names are static host identifiers; no settings, DSNs or account material
// are written to the startup timeline. Preserve the original error for callers.
func runStartupPhase(name string, run func() error) error {
	started := time.Now()
	log.Printf("startup phase=%s status=starting", name)
	err := run()
	status := "ready"
	if err != nil {
		status = "failed"
	}
	log.Printf("startup phase=%s status=%s elapsed_ms=%d", name, status, time.Since(started).Milliseconds())
	if err != nil {
		return fmt.Errorf("startup phase %s: %w", name, err)
	}
	return nil
}
