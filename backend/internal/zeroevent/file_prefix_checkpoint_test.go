package zeroevent

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestVerifiedPrefixesResumeExactlyAcrossRestartAndCodecs(t *testing.T) {
	for _, codec := range []string{CompressionNone, CompressionZstd, CompressionLZ4} {
		t.Run(codec, func(t *testing.T) {
			cfg := testFileConfig(t)
			events := []Envelope{}
			for i := 1; i <= 9; i++ {
				events = append(events, testEnvelope("prefix", uint64(i)))
			}
			if codec == CompressionNone {
				writeRawTestSegment(t, cfg.Directory, 1, events[:5])
				writeRawTestSegment(t, cfg.Directory, 2, events[5:])
			} else {
				for segment, group := range [][]Envelope{events[:5], events[5:]} {
					var raw, encoded bytes.Buffer
					for _, ev := range group {
						r, e := encodeRecord(ev)
						if e != nil {
							t.Fatal(e)
						}
						raw.Write(r)
					}
					if e := compressRawSegmentWithCodec(&raw, &encoded, 700, codec, 1, 1); e != nil {
						t.Fatal(e)
					}
					suffix := segmentZstdSuffix
					if codec == CompressionLZ4 {
						suffix = segmentLZ4Suffix
					}
					if e := os.WriteFile(segmentPath(cfg.Directory, uint64(segment+1), suffix), encoded.Bytes(), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			spool, e := NewFileSpool(cfg)
			if e != nil {
				t.Fatal(e)
			}
			if e = spool.Start(t.Context()); e != nil {
				t.Fatal(e)
			}
			batch, e := spool.ReadBatch(t.Context(), 2000)
			if e != nil || len(batch.Events) != 9 || len(batch.Positions) != 9 || batch.Positions[8] != batch.Next {
				t.Fatal(batch.Next, len(batch.Positions), e)
			}
			for _, index := range []int{1, 3, 5} {
				if e = spool.Commit(t.Context(), batch.Positions[index]); e != nil {
					t.Fatal(index, e)
				}
			}
			if e = spool.Close(); e != nil {
				t.Fatal(e)
			}
			spool, e = NewFileSpool(cfg)
			if e != nil {
				t.Fatal(e)
			}
			if e = spool.Start(context.Background()); e != nil {
				t.Fatal(e)
			}
			defer spool.Close()
			tail, e := spool.ReadBatch(t.Context(), 2000)
			if e != nil || len(tail.Events) != 3 || tail.Events[0].Sequence != 7 || len(tail.Positions) != 3 {
				t.Fatal(len(tail.Events), e)
			}
			if e = spool.Commit(t.Context(), tail.Next); e != nil {
				t.Fatal(e)
			}
		})
	}
}
