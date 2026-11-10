package zeroevent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The baseline is the former recovery path: decode/check every sealed record
// before opening the active tail. Fixtures and compression are outside timing;
// both paths see identical retained bytes and preserve all integrity checks at
// the point where events become eligible for projection.
func BenchmarkFileSpoolRecoveryHistory(b *testing.B) {
	for _, count := range []int{1, 128, 512} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			directory := b.TempDir()
			event := testEnvelope("historical", 1)
			payload, _ := json.Marshal(map[string]string{"diagnostic": strings.Repeat("synthetic old event ", 2048)})
			event.Payload = payload
			record, err := encodeRecord(event)
			if err != nil {
				b.Fatal(err)
			}
			var raw bytes.Buffer
			for range 16 {
				raw.Write(record)
			}
			var compressed bytes.Buffer
			if err := compressRawSegmentToZstd(bytes.NewReader(raw.Bytes()), &compressed, 1<<20, 1); err != nil {
				b.Fatal(err)
			}
			for i := 1; i <= count; i++ {
				if err := os.WriteFile(segmentPath(directory, uint64(i), segmentZstdSuffix), compressed.Bytes(), 0600); err != nil {
					b.Fatal(err)
				}
			}
			tail, err := encodeRecord(testEnvelope("active", 2))
			if err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(segmentPath(directory, uint64(count+1), segmentActiveSuffix), tail, 0600); err != nil {
				b.Fatal(err)
			}
			b.Run("previous_full_scan", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					segments, err := listSegments(directory)
					if err != nil {
						b.Fatal(err)
					}
					for _, segment := range segments {
						if _, _, err := inspectSegment(segment.path, segment.size, segment.active); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
			b.Run("active_tail_recovery", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					active, err := recoverSegmentsAtCheckpoint(directory, Checkpoint{})
					if err != nil {
						b.Fatal(err)
					}
					if active == nil || active.records != 1 {
						b.Fatal("active record recovery changed")
					}
					if err := active.file.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("status_headers_only", func(b *testing.B) {
				cfg := DefaultConfig()
				cfg.Directory = directory
				cfg.Compression.Enabled = false
				cfg.Compression.Algorithm = CompressionNone
				cfg.Compaction.Enabled = false
				cfg.Storage.MinFreeSpace = 0
				cfg.Storage.EmergencyReserve = 0
				spool, err := NewFileSpool(cfg)
				if err != nil {
					b.Fatal(err)
				}
				if err := spool.Start(context.Background()); err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() {
					if err := spool.Close(); err != nil {
						b.Error(err)
					}
				})
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					status := spool.Status()
					if status.PendingUnknown || status.PendingEvents != int64(count*16+1) {
						b.Fatal("metadata count mismatch", status)
					}
				}
			})

		})
	}
}
