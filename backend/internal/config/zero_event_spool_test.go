package config

import "testing"

func TestZeroEventSpoolConfigDefaultsToFile(t *testing.T) {
	c := Config{ZeroEventSpoolMode: ZeroEventSpoolFile, ZeroEventSpoolDir: "/tmp/zboard-events"}
	cfg := c.ZeroEventSpoolConfig()
	if !cfg.Enabled || cfg.Directory != "/tmp/zboard-events" {
		t.Fatalf("unexpected spool config: %+v", cfg)
	}
	if cfg.Consumer.MaxBatch != 2000 {
		t.Fatalf("unexpected consumer defaults: %+v", cfg.Consumer)
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
		default:
			return ""
		}
	})
	if c.ZeroEventSpoolMode != "legacy" || c.ZeroEventSpoolDir != "/srv/zboard/events" {
		t.Fatalf("unexpected environment overrides: mode=%q dir=%q", c.ZeroEventSpoolMode, c.ZeroEventSpoolDir)
	}
}
