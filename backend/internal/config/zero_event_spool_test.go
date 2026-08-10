package config

import (
	"strings"
	"testing"
)

func TestZeroEventSpoolConfigDefaultsToFile(t *testing.T) {
	c := Config{ZeroEventSpoolMode: ZeroEventSpoolFile, ZeroEventSpoolDir: "/tmp/zboard-events"}
	cfg := c.ZeroEventSpoolConfig()
	if !cfg.Enabled || cfg.Directory != "/tmp/zboard-events" {
		t.Fatalf("unexpected spool config: %+v", cfg)
	}
	if cfg.Consumer.MaxBatch != 2000 {
		t.Fatalf("unexpected consumer defaults: %+v", cfg.Consumer)
	}
	if cfg.Storage.MaxSize != 5<<30 || cfg.Storage.MinFreeSpace != 512<<20 || cfg.Storage.EmergencyReserve != 256<<20 || cfg.Storage.CriticalReserve != 1<<30 {
		t.Fatalf("unexpected storage defaults: %+v", cfg.Storage)
	}
}

func TestZeroEventSpoolLegacyDisablesBuffer(t *testing.T) {
	c := Config{ZeroEventSpoolMode: ZeroEventSpoolLegacy}
	if c.ZeroEventSpoolConfig().Enabled {
		t.Fatal("legacy mode must disable the file spool")
	}
}

func TestZeroEventSpoolEnvironmentOverrides(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		switch key {
		case "ZBOARD_ZERO_EVENT_SPOOL_MODE":
			return "legacy"
		case "ZBOARD_ZERO_EVENT_SPOOL_DIR":
			return "/srv/zboard/events"
		case "ZBOARD_ZERO_EVENT_SPOOL_MAX_BYTES":
			return "10737418240"
		case "ZBOARD_ZERO_EVENT_SPOOL_WARNING_RATIO":
			return "0.70"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPACT_RATIO":
			return "0.80"
		case "ZBOARD_ZERO_EVENT_SPOOL_EMERGENCY_RATIO":
			return "0.90"
		case "ZBOARD_ZERO_EVENT_SPOOL_MIN_FREE_BYTES":
			return "268435456"
		case "ZBOARD_ZERO_EVENT_SPOOL_EMERGENCY_RESERVE_BYTES":
			return "134217728"
		case "ZBOARD_ZERO_EVENT_SPOOL_CRITICAL_RESERVE_BYTES":
			return "2147483648"
		default:
			return ""
		}
	})
	if err := c.normalizeZeroEventSpoolStorage(); err != nil {
		t.Fatal(err)
	}
	if c.ZeroEventSpoolMode != "legacy" || c.ZeroEventSpoolDir != "/srv/zboard/events" {
		t.Fatalf("unexpected environment overrides: mode=%q dir=%q", c.ZeroEventSpoolMode, c.ZeroEventSpoolDir)
	}
	storage := c.ZeroEventSpoolConfig().Storage
	if storage.MaxSize != 10<<30 || storage.WarningRatio != 0.70 || storage.CompactRatio != 0.80 || storage.EmergencyRatio != 0.90 {
		t.Fatalf("unexpected capacity overrides: %+v", storage)
	}
	if storage.MinFreeSpace != 256<<20 || storage.EmergencyReserve != 128<<20 || storage.CriticalReserve != 2<<30 {
		t.Fatalf("unexpected reserve overrides: %+v", storage)
	}
}

func TestZeroEventSpoolEnvironmentAllowsZeroOptionalReserves(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		switch key {
		case "ZBOARD_ZERO_EVENT_SPOOL_MIN_FREE_BYTES", "ZBOARD_ZERO_EVENT_SPOOL_EMERGENCY_RESERVE_BYTES", "ZBOARD_ZERO_EVENT_SPOOL_CRITICAL_RESERVE_BYTES":
			return "0"
		default:
			return ""
		}
	})
	if err := c.normalizeZeroEventSpoolStorage(); err != nil {
		t.Fatal(err)
	}
	storage := c.ZeroEventSpoolConfig().Storage
	if storage.MinFreeSpace != 0 || storage.EmergencyReserve != 0 || storage.CriticalReserve != 0 {
		t.Fatalf("zero overrides were replaced by defaults: %+v", storage)
	}
}

func TestZeroEventSpoolEnvironmentRejectsMalformedStorageValue(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		if key == "ZBOARD_ZERO_EVENT_SPOOL_WARNING_RATIO" {
			return "not-a-ratio"
		}
		return ""
	})
	if err := c.normalizeZeroEventSpoolStorage(); err == nil || !strings.Contains(err.Error(), "ZBOARD_ZERO_EVENT_SPOOL_WARNING_RATIO") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestZeroEventSpoolEnvironmentRejectsInvalidRatioOrdering(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		if key == "ZBOARD_ZERO_EVENT_SPOOL_WARNING_RATIO" {
			return "0.90"
		}
		if key == "ZBOARD_ZERO_EVENT_SPOOL_COMPACT_RATIO" {
			return "0.80"
		}
		return ""
	})
	if err := c.normalizeZeroEventSpoolStorage(); err == nil || !strings.Contains(err.Error(), "warning < compact") {
		t.Fatalf("unexpected error: %v", err)
	}
}
