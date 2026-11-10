package zeroevent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func writeRecoveryEvent(t *testing.T, directory string, sequence uint64, suffix, id string) {
	t.Helper()
	record, err := encodeRecord(testEnvelope(id, sequence))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(segmentPath(directory, sequence, suffix), record, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestRecoveryDoesNotDecodeRetainedSealedHistory(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Retention.Consumed = time.Hour
	for i := uint64(1); i <= 128; i++ {
		// These committed bytes are deliberately unreadable. Startup must not open
		// them, and must not quarantine/delete evidence which is no longer pending.
		if err := os.WriteFile(segmentPath(cfg.Directory, i, segmentZstdSuffix), []byte("retained old bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeCheckpoint(cfg.Directory, Checkpoint{Segment: 129, Block: 1, Record: 2}); err != nil {
		t.Fatal(err)
	}
	writeRecoveryEvent(t, cfg.Directory, 130, segmentActiveSuffix, "pending")
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	batch, err := spool.ReadBatch(context.Background(), 1)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].ID != "pending" {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	if _, err := os.Stat(segmentPath(cfg.Directory, 1, segmentZstdSuffix)); err != nil {
		t.Fatal("startup changed retained history", err)
	}
}
func TestPendingSealedCorruptionFailsBeforeProjectionAndKeepsCheckpoint(t *testing.T) {
	for _, suffix := range []string{segmentReadySuffix, segmentZstdSuffix} {
		t.Run(suffix, func(t *testing.T) {
			cfg := testFileConfig(t)
			path := segmentPath(cfg.Directory, 1, suffix)
			if err := os.WriteFile(path, []byte("damaged pending event"), 0600); err != nil {
				t.Fatal(err)
			}
			spool, _ := NewFileSpool(cfg)
			if err := spool.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer spool.Close()
			if batch, err := spool.ReadBatch(context.Background(), 10); err == nil || len(batch.Events) != 0 {
				t.Fatal("corrupt pending event reached projector", batch, err)
			}
			if spool.checkpoint != (Checkpoint{}) {
				t.Fatal("failed read advanced cursor")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("pending evidence was removed", err)
			}
		})
	}
}
func TestRecoveryRejectsMixedActiveFilesBehindCheckpointWithoutChangingEvidence(t *testing.T) {
	directory := t.TempDir()
	writeRecoveryEvent(t, directory, 1, segmentActiveSuffix, "ambiguous-old-active")
	writeRecoveryEvent(t, directory, 3, segmentActiveSuffix, "newer-active")
	before, err := os.ReadFile(segmentPath(directory, 1, segmentActiveSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if active, err := recoverSegmentsAtCheckpoint(directory, Checkpoint{Segment: 2, Record: 1}); err == nil || active != nil {
		t.Fatal("ambiguous acknowledged active tail was accepted", err)
	}
	after, err := os.ReadFile(segmentPath(directory, 1, segmentActiveSuffix))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("ambiguous active evidence changed", err)
	}
	if _, err := os.Stat(segmentPath(directory, 3, segmentActiveSuffix)); err != nil {
		t.Fatal("newer active evidence changed", err)
	}
}
func TestWriterNeverReusesSequenceAfterCommittedReclamation(t *testing.T) {
	cfg := testFileConfig(t)
	spool, _ := NewFileSpool(cfg)
	// Exercise two single-owner writer steps, with reclamation precisely between
	// sealing and allocating the next segment. No scheduling race/test hook.
	if err := spool.writeBatch([]appendRequest{{ctx: context.Background(), event: testEnvelope("old", 1)}}); err != nil {
		t.Fatal(err)
	}
	first := spool.active.sequence
	if err := spool.sealActive(); err != nil {
		t.Fatal(err)
	}
	if err := writeCheckpoint(cfg.Directory, Checkpoint{Segment: first, Record: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(segmentPath(cfg.Directory, first, segmentReadySuffix)); err != nil {
		t.Fatal(err)
	}
	if err := spool.writeBatch([]appendRequest{{ctx: context.Background(), event: testEnvelope("new", 2)}}); err != nil {
		t.Fatal(err)
	}
	if spool.active.sequence <= first {
		t.Fatal("sequence reused after directory became empty", spool.active.sequence)
	}
	if err := spool.closeActive(); err != nil {
		t.Fatal(err)
	}
	reopened, _ := NewFileSpool(cfg)
	if err := reopened.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	batch, err := reopened.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].ID != "new" {
		t.Fatal("new acknowledged data was hidden behind checkpoint", batch, err)
	}
}
func TestEmptySpoolAllocationRespectsDurableCheckpointAndOverflow(t *testing.T) {
	cfg := testFileConfig(t)
	if err := writeCheckpoint(cfg.Directory, Checkpoint{Segment: 2975, Block: 1, Record: 1}); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := spool.Append(context.Background(), testEnvelope("new-after-reclamation", 1)); err != nil {
		t.Fatal(err)
	}
	batch, err := spool.ReadBatch(context.Background(), 1)
	if err != nil || len(batch.Events) != 1 || batch.Next.Segment != 2976 {
		t.Fatal(batch, err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	if active, err := createActiveSegment(t.TempDir(), math.MaxUint64); err == nil || active != nil {
		t.Fatal("sequence overflow was allowed")
	}
}
func TestSequenceRecoveryResumesInterruptedRenamesWithoutChangingEvents(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Retention.Consumed = time.Hour
	checkpoint := Checkpoint{Segment: 2975, Block: 1, Record: 1}
	if err := writeCheckpoint(cfg.Directory, checkpoint); err != nil {
		t.Fatal(err)
	}
	writeRecoveryEvent(t, cfg.Directory, 1, segmentReadySuffix, "first")
	writeRecoveryEvent(t, cfg.Directory, 2, segmentActiveSuffix, "second")
	plan := sequenceRecoveryPlan{Version: 1, Checkpoint: checkpoint}
	for i, suffix := range []string{segmentReadySuffix, segmentActiveSuffix} {
		from := segmentPath(cfg.Directory, uint64(i+1), suffix)
		to := segmentPath(cfg.Directory, uint64(2976+i), suffix)
		info, err := os.Stat(from)
		if err != nil {
			t.Fatal(err)
		}
		plan.Moves = append(plan.Moves, sequenceRecoveryMove{From: filepath.Base(from), To: filepath.Base(to), Size: info.Size(), Modified: info.ModTime().UnixNano()})
	}
	data, _ := json.Marshal(plan)
	if err := writeRecoveryMetadata(cfg.Directory, sequenceRecoveryJournal, data); err != nil {
		t.Fatal(err)
	}
	// Crash after one rename, before directory commit/receipt.
	if err := os.Rename(filepath.Join(cfg.Directory, plan.Moves[0].From), filepath.Join(cfg.Directory, plan.Moves[0].To)); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, event := range batch.Events {
		ids = append(ids, event.ID)
	}
	if !reflect.DeepEqual(ids, []string{"first", "second"}) {
		t.Fatal("events lost or altered", ids)
	}
	if spool.checkpoint != checkpoint {
		t.Fatal("recovery advanced accounting checkpoint")
	}
	if _, err := os.Stat(filepath.Join(cfg.Directory, sequenceRecoveryJournal)); !os.IsNotExist(err) {
		t.Fatal("unfinished journal remained", err)
	}
	var receipt sequenceRecoveryResult
	data, err = os.ReadFile(filepath.Join(cfg.Directory, sequenceRecoveryReceipt))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.PreviousCheckpoint != checkpoint || receipt.Segments != 2 || receipt.LastSegment != 2977 {
		t.Fatal(receipt)
	}
	if err := spool.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _ := NewFileSpool(cfg)
	if err := reopened.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	batch, err = reopened.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 0 {
		t.Fatal("completed replay repeated after restart", batch, err)
	}
}
func TestSequenceRecoveryRejectsConflictingEvidenceBeforeAnyRename(t *testing.T) {
	for _, fault := range []string{"target_collision", "changed_source", "changed_checkpoint", "path_escape"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			cp := Checkpoint{Segment: 10, Record: 1}
			writeRecoveryEvent(t, dir, 1, segmentActiveSuffix, "retained")
			source := segmentPath(dir, 1, segmentActiveSuffix)
			info, _ := os.Stat(source)
			plan := sequenceRecoveryPlan{Version: 1, Checkpoint: cp, Moves: []sequenceRecoveryMove{{From: filepath.Base(source), To: filepath.Base(segmentPath(dir, 11, segmentActiveSuffix)), Size: info.Size(), Modified: info.ModTime().UnixNano()}}}
			switch fault {
			case "target_collision":
				writeRecoveryEvent(t, dir, 11, segmentActiveSuffix, "conflict")
			case "changed_source":
				if err := os.WriteFile(source, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed_checkpoint":
				cp.Record++
			case "path_escape":
				plan.Moves[0].To = "../escaped.active"
			}
			if err := finishSequenceRecovery(dir, cp, plan); err == nil {
				t.Fatal("conflicting recovery accepted")
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatal("failure removed source evidence", err)
			}
		})
	}
}
func TestFreshLegacySequenceRecoveryPreservesCompressedPayloadAndIDs(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Retention.Consumed = time.Hour
	// Raw and compressed segments use the same physical rebase; payloads are not
	// decoded or re-encoded while changing their durable sequence namespace.
	raw := segmentPath(cfg.Directory, 1, segmentReadySuffix)
	writeRecoveryEvent(t, cfg.Directory, 1, segmentReadySuffix, "compressed-original")
	file, err := os.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Create(segmentPath(cfg.Directory, 1, segmentZstdSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if err := compressRawSegmentToZstd(file, target, 64<<10, 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	target.Close()
	os.Remove(raw)
	writeRecoveryEvent(t, cfg.Directory, 2, segmentActiveSuffix, "active-original")
	if err := writeCheckpoint(cfg.Directory, Checkpoint{Segment: 100, Block: 1, Record: 1}); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 2 {
		t.Fatal(batch, err)
	}
	for i, want := range []string{"compressed-original", "active-original"} {
		if batch.Events[i].ID != want {
			t.Fatal(fmt.Sprintf("ID changed: %s", batch.Events[i].ID))
		}
	}
}

func TestRecoverySealsNonTailActiveBeforeAcceptingNewAppends(t *testing.T) {
	cfg := testFileConfig(t)
	writeRecoveryEvent(t, cfg.Directory, 2, segmentActiveSuffix, "older-active")
	writeRecoveryEvent(t, cfg.Directory, 3, segmentReadySuffix, "later-sealed")
	if err := writeCheckpoint(cfg.Directory, Checkpoint{Segment: 1, Record: 1}); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 2 {
		t.Fatal(batch, err)
	}
	if err := spool.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	if err := spool.Append(context.Background(), testEnvelope("fresh-after-cursor", 4)); err != nil {
		t.Fatal(err)
	}
	batch, err = spool.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].ID != "fresh-after-cursor" || batch.Next.Segment != 4 {
		t.Fatal("append went behind consumed horizon", batch, err)
	}
}
