package jobstore

import (
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestDispatchGroupAdmissionKeepsMissingZeroFullAndResourceFences(t *testing.T) {
	a, b := fixture(t, 8)
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = a.now
	for _, group := range []ExecutionGroup{{ID: "missing", Capacity: 1}, {ID: "zero", Capacity: 1}, {ID: "full", Capacity: 1}, {ID: "free", Capacity: 1}} {
		if err := a.db.Create(&group).Error; err != nil {
			t.Fatal(err)
		}
	}
	add := func(key, group, resource string) jobs.Run {
		t.Helper()
		r, err := a.Submit(t.Context(), jobs.Submission{Owner: "system", Key: key, Handler: key, ExecutionGroup: group, Resource: resource, Payload: `{}`})
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Millisecond)
		return r
	}
	add("occupy-full", "full", "shared-resource")
	busy, err := b.Claim(t.Context(), "foreign-worker", []string{"occupy-full"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ key, group, resource string }{
		{"missing", "missing", ""}, {"zero", "zero", ""}, {"full", "full", ""},
		{"resource-busy", "", "shared-resource"}, {"free-one", "free", ""}, {"free-two", "free", ""},
	} {
		add(spec.key, spec.group, spec.resource)
	}
	// Queue valid work first, then simulate a removed or disabled group. The
	// normal submission guard correctly refuses absent/zero groups up front.
	if err := a.db.Delete(&ExecutionGroup{}, "id = ?", "missing").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&ExecutionGroup{}).Where("id = ?", "zero").Update("capacity", 0).Error; err != nil {
		t.Fatal(err)
	}
	handlers := []string{"missing", "zero", "full", "resource-busy", "free-one", "free-two"}
	first, err := a.Claim(t.Context(), "local-worker", handlers, time.Minute)
	if err != nil || first.Run.Handler != "free-one" {
		t.Fatal("ineligible older work blocked free group", first, err)
	}
	if _, err := b.Claim(t.Context(), "foreign-worker", handlers, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("missing, zero, full or resource-busy admission bypassed", err)
	}
	if err := a.Finish(t.Context(), first, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	second, err := b.Claim(t.Context(), "foreign-worker", handlers, time.Minute)
	if err != nil || second.Run.Handler != "free-two" {
		t.Fatal("completed lease did not free exactly one group slot", second, err)
	}
	if err := b.Finish(t.Context(), busy, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	// An unknown execution still owns its resource; no guessed completion can
	// make shared-resource available. Unknown is not a running group slot.
	if _, err := a.Claim(t.Context(), "local-worker", []string{"resource-busy"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("unknown resource fence bypassed", err)
	}
	if _, err := a.Claim(t.Context(), "local-worker", []string{"full"}, time.Minute); err != nil {
		t.Fatal("independent group slot incorrectly charged to unknown execution", err)
	}
}
