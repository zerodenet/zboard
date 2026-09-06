package zeroevent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Isolate steady consumption cost as retained history grows. Recovery and
// fixture construction are excluded; checksum/sequence checks, checkpoint
// persistence and the consumer's periodic status reads remain measured.
func BenchmarkFileSpoolDrain(b *testing.B) {
	for _, count := range []int{256, 1024, 4096} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			b.StopTimer()
			for range b.N {
				cfg := DefaultConfig()
				cfg.Directory = b.TempDir()
				cfg.Compression.Enabled = false
				cfg.Compression.Algorithm = CompressionNone
				cfg.Storage.MinFreeSpace, cfg.Storage.EmergencyReserve = 0, 0
				cfg.Retention.Consumed = time.Hour
				var contents bytes.Buffer
				for i := range count {
					record, err := encodeRecord(testEnvelope(fmt.Sprint(i), uint64(i+1)))
					if err != nil {
						b.Fatal(err)
					}
					contents.Write(record)
				}
				if err := os.WriteFile(segmentPath(cfg.Directory, 1, segmentReadySuffix), contents.Bytes(), 0600); err != nil {
					b.Fatal(err)
				}
				spool, err := NewFileSpool(cfg)
				if err != nil {
					b.Fatal(err)
				}
				if err := spool.Start(context.Background()); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				consumed := 0
				for {
					batch, err := spool.ReadBatch(context.Background(), 32)
					if err != nil {
						b.Fatal(err)
					}
					if len(batch.Events) == 0 {
						break
					}
					for _, event := range batch.Events {
						consumed++
						if event.Sequence != uint64(consumed) {
							b.Fatal("record sequence skipped or repeated")
						}
					}
					if err := spool.Commit(context.Background(), batch.Next); err != nil {
						b.Fatal(err)
					}
					if consumed%256 == 0 {
						_ = spool.Status()
					}
				}
				b.StopTimer()
				if consumed != count {
					b.Fatalf("consumed=%d want=%d", consumed, count)
				}
				if err := spool.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.N*count)/b.Elapsed().Seconds(), "events/s")
		})
	}
}
