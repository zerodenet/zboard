package zeroevent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DriverFile = "file"

	CompressionNone = "none"
	CompressionZstd = "zstd"
	CompressionLZ4  = "lz4"
)

type Envelope struct {
	ID             string          `json:"id,omitempty"`
	NodeID         uint64          `json:"node_id"`
	SourceID       string          `json:"source_id,omitempty"`
	PrincipalKey   string          `json:"principal_key,omitempty"`
	Type           string          `json:"type"`
	OccurredAt     time.Time       `json:"occurred_at"`
	CoreInstanceID string          `json:"core_instance_id,omitempty"`
	ConfigRevision uint64          `json:"config_revision,omitempty"`
	FlowID         string          `json:"flow_id,omitempty"`
	Sequence       uint64          `json:"sequence,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

// RuntimeCursor identifies an event position within one Zero engine instance.
// Sequence values are not comparable across different core instances because
// the kernel resets its sequence after a real engine restart.
type RuntimeCursor struct {
	CoreInstanceID string `json:"core_instance_id"`
	Sequence       uint64 `json:"sequence"`
}

func (e Envelope) RuntimeCursor() (RuntimeCursor, bool) {
	instanceID := strings.TrimSpace(e.CoreInstanceID)
	if instanceID == "" || e.Sequence == 0 {
		return RuntimeCursor{}, false
	}
	return RuntimeCursor{CoreInstanceID: instanceID, Sequence: e.Sequence}, true
}

type Checkpoint struct {
	Segment uint64 `json:"segment"`
	Block   uint64 `json:"block"`
	Record  uint64 `json:"record"`
}

type Batch struct {
	Events []Envelope
	Next   Checkpoint
}

type Status struct {
	Driver        string     `json:"driver"`
	PendingEvents int64      `json:"pending_events"`
	PendingBytes  int64      `json:"pending_bytes"`
	Segments      int        `json:"segments"`
	OldestEventAt *time.Time `json:"oldest_event_at,omitempty"`
	Emergency     bool       `json:"emergency"`
}

type EventAppender interface {
	Append(context.Context, Envelope) error
}

type EventConsumer interface {
	ReadBatch(context.Context, int) (Batch, error)
	Commit(context.Context, Checkpoint) error
}

type EventSpool interface {
	EventAppender
	EventConsumer
	Start(context.Context) error
	Status() Status
	Close() error
}

type Config struct {
	Enabled     bool
	Driver      string
	Directory   string
	Segment     SegmentConfig
	Flush       FlushConfig
	Consumer    ConsumerConfig
	Compression CompressionConfig
	Storage     StorageConfig
	Retention   RetentionConfig
}

type SegmentConfig struct {
	MaxSize int64
	MaxAge  time.Duration
}

type FlushConfig struct {
	Interval time.Duration
	MaxBatch int
}

type ConsumerConfig struct {
	CommitInterval time.Duration
	MaxBatch       int
}

type CompressionConfig struct {
	Enabled   bool
	Algorithm string
	Level     int
	BlockSize int64
	Workers   int
}

type StorageConfig struct {
	MaxSize          int64
	WarningRatio     float64
	CompactRatio     float64
	EmergencyRatio   float64
	MinFreeSpace     int64
	EmergencyReserve int64
	CriticalReserve  int64
}

type RetentionConfig struct {
	Consumed time.Duration
}

func DefaultConfig() Config {
	return Config{
		Enabled:   true,
		Driver:    DriverFile,
		Directory: "/var/lib/zboard/zero-events",
		Segment: SegmentConfig{
			MaxSize: 64 << 20,
			MaxAge:  10 * time.Minute,
		},
		Flush: FlushConfig{
			Interval: 10 * time.Millisecond,
			MaxBatch: 256,
		},
		Consumer: ConsumerConfig{
			CommitInterval: 5 * time.Second,
			MaxBatch:       2000,
		},
		Compression: CompressionConfig{
			Enabled:   true,
			Algorithm: CompressionZstd,
			Level:     1,
			BlockSize: 2 << 20,
			Workers:   1,
		},
		Storage: StorageConfig{
			MaxSize:          5 << 30,
			WarningRatio:     0.75,
			CompactRatio:     0.85,
			EmergencyRatio:   0.95,
			MinFreeSpace:     512 << 20,
			EmergencyReserve: 256 << 20,
			CriticalReserve:  1 << 30,
		},
		Retention: RetentionConfig{
			Consumed: 10 * time.Minute,
		},
	}
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Driver) != DriverFile {
		return fmt.Errorf("unsupported driver %q", c.Driver)
	}
	if strings.TrimSpace(c.Directory) == "" {
		return errors.New("directory is required")
	}
	if c.Segment.MaxSize <= 0 {
		return errors.New("segment max size must be greater than zero")
	}
	if c.Segment.MaxAge <= 0 {
		return errors.New("segment max age must be greater than zero")
	}
	if c.Flush.Interval <= 0 {
		return errors.New("flush interval must be greater than zero")
	}
	if c.Flush.MaxBatch <= 0 {
		return errors.New("flush max batch must be greater than zero")
	}
	if c.Consumer.CommitInterval <= 0 {
		return errors.New("consumer commit interval must be greater than zero")
	}
	if c.Consumer.MaxBatch <= 0 {
		return errors.New("consumer max batch must be greater than zero")
	}
	if c.Compression.Enabled {
		switch strings.TrimSpace(c.Compression.Algorithm) {
		case CompressionZstd, CompressionLZ4:
		default:
			return fmt.Errorf("unsupported compression algorithm %q", c.Compression.Algorithm)
		}
		if c.Compression.BlockSize <= 0 {
			return errors.New("compression block size must be greater than zero")
		}
		if c.Compression.Workers <= 0 {
			return errors.New("compression workers must be greater than zero")
		}
	}
	if c.Storage.MaxSize <= 0 {
		return errors.New("storage max size must be greater than zero")
	}
	if !(0 < c.Storage.WarningRatio &&
		c.Storage.WarningRatio < c.Storage.CompactRatio &&
		c.Storage.CompactRatio < c.Storage.EmergencyRatio &&
		c.Storage.EmergencyRatio < 1) {
		return errors.New("storage ratios must satisfy 0 < warning < compact < emergency < 1")
	}
	if c.Storage.MinFreeSpace < 0 || c.Storage.EmergencyReserve < 0 || c.Storage.CriticalReserve < 0 {
		return errors.New("storage reserve sizes must not be negative")
	}
	if c.Storage.EmergencyReserve > c.Storage.MaxSize {
		return errors.New("emergency reserve must not exceed storage max size")
	}
	if c.Storage.CriticalReserve > c.Storage.MaxSize {
		return errors.New("critical reserve must not exceed storage max size")
	}
	if c.Retention.Consumed < 0 {
		return errors.New("consumed retention must not be negative")
	}
	return nil
}
