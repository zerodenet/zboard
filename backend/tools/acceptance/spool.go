package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

// Only called by the runner after the server container has stopped. Opening a
// second writer against a live spool is unsafe and is never part of sampling.
func verifySpool(dir string) error {
	path := filepath.Join(dir, "events")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cfg := zeroevent.DefaultConfig()
	cfg.Directory = path
	cfg.Compression.Enabled = false
	cfg.Compression.Algorithm = zeroevent.CompressionNone
	cfg.Storage.MinFreeSpace = 0
	cfg.Storage.EmergencyReserve = 0
	spool, err := zeroevent.NewFileSpool(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := spool.Start(ctx); err != nil {
		return err
	}
	batch, readErr := spool.ReadBatch(ctx, 1)
	status := spool.Status()
	closeErr := spool.Close()
	passed := readErr == nil && closeErr == nil && len(batch.Events) == 0
	if err := writeJSON(filepath.Join(dir, "spool-verification.json"), map[string]any{
		"verified_at": time.Now().UTC(), "passed": passed, "status": status,
		"unconsumed_event_found": len(batch.Events) != 0,
	}); err != nil {
		return err
	}
	if !passed {
		return fmt.Errorf("spool not drained: events=%d read=%v close=%v", len(batch.Events), readErr, closeErr)
	}
	return nil
}
