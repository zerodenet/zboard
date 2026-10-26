package application

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"testing"
)

type continuationRepository struct {
	count   int
	failure error
	calls   int
	limit   int
	budget  int64
}

func (r *continuationRepository) HasPending(context.Context) (bool, error) { return true, nil }
func (r *continuationRepository) Drain(_ context.Context, limit int) (int, error) {
	r.calls++
	r.limit = limit
	return r.count, r.failure
}

type continuationObservations struct{ *continuationRepository }

func (r continuationObservations) Drain(_ context.Context, limit int, budget int64) (int, error) {
	r.calls++
	r.limit = limit
	r.budget = budget
	return r.count, r.failure
}
func (r continuationObservations) Retire(_ context.Context, limit int, budget int64) (network.ObservationMaterialRetentionResult, error) {
	r.calls++
	r.limit = limit
	r.budget = budget
	return network.ObservationMaterialRetentionResult{Inspected: r.count}, r.failure
}

func TestProjectionContinuationPreservesBudgetsAndFailures(t *testing.T) {
	failure := errors.New("projection transaction or checkpoint failed")
	for _, kind := range []string{"usage", "observation", "retention"} {
		limit := 32
		if kind == "usage" {
			limit = 200
		}
		for _, test := range []struct {
			name    string
			count   int
			failure error
			yield   bool
		}{{"full", limit, nil, true}, {"partial", limit - 1, nil, false}, {"empty", 0, nil, false}, {"failed-full", limit, failure, false}} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				repo := &continuationRepository{count: test.count, failure: test.failure}
				s := &Services{UsageProjections: metering.UsageProjections{Repository: repo}, NodeObservations: network.ObservationProjections{Repository: continuationObservations{repo}}, NodeObservationMaterialRetention: network.ObservationMaterialRetention{Repository: continuationObservations{repo}}}
				job := s.UsageProjectionJob()
				if kind == "observation" {
					job = s.NodeObservationJob()
				}
				if kind == "retention" {
					job = s.NodeObservationMaterialRetentionJob()
				}
				err := job.Run(t.Context())
				if jobs.IsContinuation(err) != test.yield || test.failure != nil && !errors.Is(err, failure) || test.failure == nil && !test.yield && err != nil {
					t.Fatal("incorrect bounded continuation outcome", err)
				}
				if repo.calls != 1 || repo.limit != limit || kind != "usage" && repo.budget != 8*1024*1024 {
					t.Fatal("continuation changed execution budget or privately looped", repo)
				}
			})
		}
	}
}
