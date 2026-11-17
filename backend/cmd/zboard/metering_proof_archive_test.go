package main

import (
	"flag"
	"io"
	"testing"
	"time"
)

func TestMeteringProofMaintenanceFlagsRejectOrphansAndModeConflicts(t *testing.T) {
	before := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	for _, tc := range []struct {
		name             string
		args             []string
		other, wantError bool
	}{
		{"ordinary", nil, false, false},
		{"default_orphan", []string{"-archive-metering-proof-batch=32"}, false, true},
		{"zero_restore", []string{"-restore-metering-proof=0"}, false, true},
		{"missing_range", []string{"-archive-metering-proof"}, false, true},
		{"archive", []string{"-archive-metering-proof", "-archive-metering-proof-through=4", "-archive-metering-proof-before=" + before}, false, false},
		{"two_modes", []string{"-archive-metering-proof", "-restore-metering-proof=1", "-archive-metering-proof-through=4", "-archive-metering-proof-before=" + before}, false, true},
		{"other_mode", []string{"-restore-metering-proof=1"}, true, true},
		{"restore", []string{"-restore-metering-proof=1"}, false, false},
		{"restore_with_range", []string{"-restore-metering-proof=1", "-archive-metering-proof-after=0"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldArchive, oldRestore, oldAfter, oldThrough, oldBefore, oldBatch := *archiveMeteringProof, *restoreMeteringProof, *archiveMeteringProofAfter, *archiveMeteringProofThrough, *archiveMeteringProofBefore, *archiveMeteringProofBatch
			t.Cleanup(func() {
				*archiveMeteringProof = oldArchive
				*restoreMeteringProof = oldRestore
				*archiveMeteringProofAfter = oldAfter
				*archiveMeteringProofThrough = oldThrough
				*archiveMeteringProofBefore = oldBefore
				*archiveMeteringProofBatch = oldBatch
			})
			fs := flag.NewFlagSet("proof", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			fs.BoolVar(archiveMeteringProof, "archive-metering-proof", false, "")
			fs.UintVar(restoreMeteringProof, "restore-metering-proof", 0, "")
			fs.UintVar(archiveMeteringProofAfter, "archive-metering-proof-after", 0, "")
			fs.UintVar(archiveMeteringProofThrough, "archive-metering-proof-through", 0, "")
			fs.StringVar(archiveMeteringProofBefore, "archive-metering-proof-before", "", "")
			fs.IntVar(archiveMeteringProofBatch, "archive-metering-proof-batch", 32, "")
			if err := fs.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			if err := validateMeteringProofFlags(fs, tc.other); (err != nil) != tc.wantError {
				t.Fatal("maintenance admission", err, tc.wantError)
			}
		})
	}
}
