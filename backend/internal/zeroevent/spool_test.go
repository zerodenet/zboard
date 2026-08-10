package zeroevent

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !c.Enabled || c.Driver != DriverFile || c.Directory == "" {
		t.Fatalf("unexpected default identity: %+v", c)
	}
	if c.Segment.MaxSize != 64<<20 || c.Segment.MaxAge != 10*time.Minute {
		t.Fatalf("unexpected segment defaults: %+v", c.Segment)
	}
	if c.Flush.Interval != 10*time.Millisecond || c.Flush.MaxBatch != 256 {
		t.Fatalf("unexpected flush defaults: %+v", c.Flush)
	}
	if c.Consumer.CommitInterval != 5*time.Second || c.Consumer.MaxBatch != 2000 {
		t.Fatalf("unexpected consumer defaults: %+v", c.Consumer)
	}
	if !c.Compression.Enabled || c.Compression.Algorithm != CompressionZstd || c.Compression.Level != 1 || c.Compression.BlockSize != 2<<20 {
		t.Fatalf("unexpected compression defaults: %+v", c.Compression)
	}
	if c.Storage.MaxSize != 5<<30 || c.Storage.WarningRatio != 0.75 || c.Storage.CompactRatio != 0.85 || c.Storage.EmergencyRatio != 0.95 {
		t.Fatalf("unexpected storage defaults: %+v", c.Storage)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{name: "driver", edit: func(c *Config) { c.Driver = "memory" }},
		{name: "segment size", edit: func(c *Config) { c.Segment.MaxSize = 0 }},
		{name: "flush batch", edit: func(c *Config) { c.Flush.MaxBatch = 0 }},
		{name: "compression", edit: func(c *Config) { c.Compression.Algorithm = "gzip" }},
		{name: "ratios", edit: func(c *Config) { c.Storage.WarningRatio = c.Storage.CompactRatio }},
		{name: "critical reserve", edit: func(c *Config) { c.Storage.CriticalReserve = c.Storage.MaxSize + 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			tt.edit(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func TestDisabledConfigDoesNotRequireFileSettings(t *testing.T) {
	c := Config{Enabled: false}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestEnvelopeRuntimeCursorIsScopedToCoreInstance(t *testing.T) {
	first := Envelope{CoreInstanceID: "core-a", Sequence: 17}
	second := Envelope{CoreInstanceID: "core-b", Sequence: 17}

	firstCursor, ok := first.RuntimeCursor()
	if !ok {
		t.Fatal("first RuntimeCursor() unavailable")
	}
	secondCursor, ok := second.RuntimeCursor()
	if !ok {
		t.Fatal("second RuntimeCursor() unavailable")
	}
	if firstCursor == secondCursor {
		t.Fatalf("runtime cursors from different core instances must differ: %+v", firstCursor)
	}
	if firstCursor.Sequence != secondCursor.Sequence {
		t.Fatalf("test requires reused sequence, got %d and %d", firstCursor.Sequence, secondCursor.Sequence)
	}
}

func TestEnvelopeRuntimeCursorRequiresGenerationIdentity(t *testing.T) {
	if _, ok := (Envelope{Sequence: 1}).RuntimeCursor(); ok {
		t.Fatal("RuntimeCursor() should reject a sequence without core_instance_id")
	}
	if _, ok := (Envelope{CoreInstanceID: "core-a"}).RuntimeCursor(); ok {
		t.Fatal("RuntimeCursor() should reject a core instance without sequence")
	}
}
