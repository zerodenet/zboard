package jobstore

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestWeightedDispatchPreventsBusyLaneStarvation(t *testing.T) {
	store, _ := fixture(t, 1)
	ctx := context.Background()
	for i := 0; i < 80; i++ {
		if _, err := store.Submit(ctx, jobs.Submission{Owner: "system", Key: fmt.Sprintf("core-%03d", i), Handler: "core", Payload: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"plugin:a", "plugin:b"} {
		for i := 0; i < 20; i++ {
			if _, err := store.Submit(ctx, jobs.Submission{Owner: owner, Key: fmt.Sprintf("work-%03d", i), Handler: owner, ExecutionGroup: "external", Payload: `{}`}); err != nil {
				t.Fatal(err)
			}
		}
	}
	counts := map[string]int{}
	last := map[string]int{"plugin:a": -1, "plugin:b": -1}
	maxGap := map[string]int{}
	for i := 0; i < 60; i++ {
		claim, err := store.Claim(ctx, "worker", []string{"core", "plugin:a", "plugin:b"}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		counts[claim.Run.Owner]++
		if claim.Run.Owner != "system" {
			if last[claim.Run.Owner] >= 0 && i-last[claim.Run.Owner] > maxGap[claim.Run.Owner] {
				maxGap[claim.Run.Owner] = i - last[claim.Run.Owner]
			}
			last[claim.Run.Owner] = i
		}
		if err := store.Finish(ctx, claim, jobs.Succeeded); err != nil {
			t.Fatal(err)
		}
	}
	if counts["system"] != 40 || counts["plugin:a"] != 10 || counts["plugin:b"] != 10 {
		t.Fatalf("weighted distribution = %#v", counts)
	}
	if maxGap["plugin:a"] > 6 || maxGap["plugin:b"] > 6 {
		t.Fatalf("plugin lane starved: gaps=%#v", maxGap)
	}
}

func TestDispatchCapacityP95WithBacklog(t *testing.T) {
	store, _ := fixture(t, 1)
	ctx := context.Background()
	handlers := []string{"core", "plugin:a", "plugin:b", "plugin:c"}
	for i := 0; i < 240; i++ {
		owner := handlers[i%len(handlers)]
		group := "external"
		if owner == "core" {
			owner = "system"
			group = ""
		}
		if _, err := store.Submit(ctx, jobs.Submission{Owner: owner, Key: fmt.Sprintf("capacity-%03d", i), Handler: handlers[i%len(handlers)], ExecutionGroup: group, Payload: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	latencies := make([]time.Duration, 0, 240)
	for range 240 {
		started := time.Now()
		claim, err := store.Claim(ctx, "capacity-worker", handlers, time.Minute)
		latencies = append(latencies, time.Since(started))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, claim, jobs.Succeeded); err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := latencies[(len(latencies)*95+99)/100-1]
	t.Logf("local SQLite backlog=240 claim_p95=%s", p95)
	if p95 > 500*time.Millisecond {
		t.Fatalf("local SQLite claim p95 %s exceeds 500ms capacity guard", p95)
	}
}
