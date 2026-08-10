package config

import (
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestZeroEventCompressionDefaults(t *testing.T) {
	cfg := Config{}.ZeroEventSpoolConfig().Compression
	if !cfg.Enabled || cfg.Algorithm != zeroevent.CompressionZstd || cfg.Level != 1 || cfg.BlockSize != 2<<20 || cfg.Workers != 1 {
		t.Fatalf("unexpected compression defaults: %+v", cfg)
	}
}

func TestZeroEventCompressionEnvironmentOverridesLZ4(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		switch key {
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_ALGORITHM":
			return "lz4"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_LEVEL":
			return "0"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_BLOCK_BYTES":
			return "1048576"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_WORKERS":
			return "2"
		default:
			return ""
		}
	})
	if err := c.normalizeZeroEventSpoolCompression(); err != nil {
		t.Fatal(err)
	}
	cfg := c.ZeroEventSpoolConfig().Compression
	if !cfg.Enabled || cfg.Algorithm != zeroevent.CompressionLZ4 || cfg.Level != 0 || cfg.BlockSize != 1<<20 || cfg.Workers != 2 {
		t.Fatalf("unexpected lz4 compression config: %+v", cfg)
	}
}

func TestZeroEventCompressionNoneDisablesCompression(t *testing.T) {
	c := Config{ZeroEventSpoolCompressionAlgorithm: zeroevent.CompressionNone}
	if err := c.normalizeZeroEventSpoolCompression(); err != nil {
		t.Fatal(err)
	}
	cfg := c.ZeroEventSpoolConfig().Compression
	if cfg.Enabled || cfg.Algorithm != zeroevent.CompressionNone {
		t.Fatalf("none algorithm did not disable compression: %+v", cfg)
	}
}

func TestZeroEventCompressionEnvironmentCanDisableCompression(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		if key == "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_ENABLED" {
			return "false"
		}
		return ""
	})
	if err := c.normalizeZeroEventSpoolCompression(); err != nil {
		t.Fatal(err)
	}
	if c.ZeroEventSpoolConfig().Compression.Enabled {
		t.Fatal("explicit compression disable was ignored")
	}
}

func TestZeroEventCompressionRejectsInvalidAlgorithm(t *testing.T) {
	c := Config{ZeroEventSpoolCompressionAlgorithm: "gzip"}
	if err := c.normalizeZeroEventSpoolCompression(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestZeroEventCompressionRejectsInvalidLZ4Level(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		switch key {
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_ALGORITHM":
			return "lz4"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_LEVEL":
			return "10"
		default:
			return ""
		}
	})
	if err := c.normalizeZeroEventSpoolCompression(); err == nil || !strings.Contains(err.Error(), "between 0 and 9") {
		t.Fatalf("unexpected error: %v", err)
	}
}
