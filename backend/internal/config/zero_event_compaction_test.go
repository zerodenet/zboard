package config

import (
	"strings"
	"testing"
)

func TestZeroEventSpoolCompactionDefaultsEnabled(t *testing.T) {
	cfg := (Config{}).ZeroEventSpoolConfig()
	if !cfg.Compaction.Enabled || !cfg.Compaction.MergeFlowUpdates || !cfg.Compaction.MergeNodeStats {
		t.Fatalf("unexpected compaction defaults: %+v", cfg.Compaction)
	}
}

func TestZeroEventSpoolCompactionEnvironmentOverrides(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		switch key {
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_ENABLED":
			return "true"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_FLOW_UPDATES":
			return "false"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_NODE_STATS":
			return "true"
		default:
			return ""
		}
	})
	if err := c.normalizeZeroEventSpoolCompaction(); err != nil {
		t.Fatal(err)
	}
	compaction := c.ZeroEventSpoolConfig().Compaction
	if !compaction.Enabled || compaction.MergeFlowUpdates || !compaction.MergeNodeStats {
		t.Fatalf("unexpected compaction override: %+v", compaction)
	}
}

func TestZeroEventSpoolCompactionCanBeDisabled(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		switch key {
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_ENABLED", "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_FLOW_UPDATES", "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_NODE_STATS":
			return "false"
		default:
			return ""
		}
	})
	if err := c.normalizeZeroEventSpoolCompaction(); err != nil {
		t.Fatal(err)
	}
	compaction := c.ZeroEventSpoolConfig().Compaction
	if compaction.Enabled || compaction.MergeFlowUpdates || compaction.MergeNodeStats {
		t.Fatalf("compaction disable override was not preserved: %+v", compaction)
	}
}

func TestZeroEventSpoolCompactionRejectsMalformedBoolean(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		if key == "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_ENABLED" {
			return "sometimes"
		}
		return ""
	})
	if err := c.normalizeZeroEventSpoolCompaction(); err == nil || !strings.Contains(err.Error(), "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_ENABLED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestZeroEventSpoolCompactionRequiresStrategyWhenEnabled(t *testing.T) {
	c := Config{}
	c.ApplyEnvironment(func(key string) string {
		switch key {
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_ENABLED":
			return "true"
		case "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_FLOW_UPDATES", "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_NODE_STATS":
			return "false"
		default:
			return ""
		}
	})
	if err := c.normalizeZeroEventSpoolCompaction(); err == nil || !strings.Contains(err.Error(), "at least one merge strategy") {
		t.Fatalf("unexpected error: %v", err)
	}
}
