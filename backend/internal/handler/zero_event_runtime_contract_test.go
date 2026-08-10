package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestZeroProjectorCrossGenerationOrderingDoesNotCompareResetSequence(t *testing.T) {
	base := time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC)
	oldGeneration := zeroevent.Envelope{ID: "old", CoreInstanceID: "core-a", Sequence: 1000, OccurredAt: base}
	newGeneration := zeroevent.Envelope{ID: "new", CoreInstanceID: "core-b", Sequence: 1, OccurredAt: base.Add(time.Second)}
	if !zeroEventNewer(newGeneration, oldGeneration) {
		t.Fatal("new generation must not lose because its sequence reset")
	}
}
