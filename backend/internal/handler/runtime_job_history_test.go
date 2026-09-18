package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func TestJobResolutionRequiresAdminAndAuditsVerifiedOutcome(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	store := jobstore.New(h.db)
	run, err := store.Submit(ctx, jobs.Submission{Owner: "system", Key: "verify", Handler: "test", Payload: `{"secret":"must-not-leak"}`})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.Claim(ctx, "test", []string{"test"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, claim, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	request := func(token string) *httptest.ResponseRecorder {
		r := pathvar.WithVars(announcementRequest("POST", "/api/v1/admin/runtime-jobs/runs/"+run.ID+"/resolve", token, `{"outcome":"succeeded","reason":"verified provider receipt"}`), map[string]string{"id": run.ID})
		w := httptest.NewRecorder()
		h.AdminRuntimeResolveHandler(w, r)
		return w
	}
	if w := request(token); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if err := h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err = h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if w := request(token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	var n int64
	h.db.Model(&model.AuditLog{}).Where("action = ?", "job.resolve").Count(&n)
	if n != 1 {
		t.Fatal("resolution audit", n)
	}
	w := httptest.NewRecorder()
	h.AdminRuntimeHistoryHandler(w, announcementRequest("GET", "/api/v1/admin/runtime-jobs/runs", token, ""))
	if w.Code != 200 || strings.Contains(w.Body.String(), "must-not-leak") || !strings.Contains(w.Body.String(), run.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestJobReviewServiceRechecksAuthorityAndRollsBackOnAuditFailure(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	store := jobstore.New(h.db)
	run, err := store.Submit(ctx, jobs.Submission{Owner: "system", Key: "review-atomic", Handler: "test", Payload: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.Claim(ctx, "test", []string{"test"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, claim, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	service := jobs.ReviewService{Repository: store}
	review := jobs.Review{RunID: run.ID, Outcome: jobs.Succeeded, Reason: "provider receipt checked"}
	if err := service.Resolve(ctx, jobs.Reviewer{AccountID: 1}, review); !errors.Is(err, jobs.ErrPermission) {
		t.Fatal("non-admin capability caller", err)
	}
	if err := h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec("CREATE TRIGGER reject_job_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'job.resolve' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Resolve(ctx, jobs.Reviewer{AccountID: 1}, review); err == nil {
		t.Fatal("expected audit failure")
	}
	var row jobstore.Record
	if err := h.db.First(&row, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != string(jobs.Unknown) {
		t.Fatal("released resource without audit", row.State)
	}
}

func TestRuntimeCancellationRequiresAdminAuditsAndExposesAttempts(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	store := jobstore.New(h.db)
	run, err := store.Submit(context.Background(), jobs.Submission{Owner: "system", Key: "cancel-active", Handler: "test", Payload: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.Claim(context.Background(), "worker-a", []string{"test"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest := func() *httptest.ResponseRecorder {
		r := pathvar.WithVars(announcementRequest("POST", "/api/v1/admin/runtime-jobs/runs/"+run.ID+"/cancel", token, `{"reason":"operator shutdown"}`), map[string]string{"id": run.ID})
		w := httptest.NewRecorder()
		h.AdminRuntimeCancelHandler(w, r)
		return w
	}
	if w := cancelRequest(); w.Code != 200 || !strings.Contains(w.Body.String(), "cancel_requested") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := cancelRequest(); w.Code != 200 {
		t.Fatal("idempotent cancellation", w.Code, w.Body.String())
	}
	var audits int64
	if err := h.db.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "job.cancel", "job:"+run.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("cancellation audit", audits, err)
	}
	attemptRequest := pathvar.WithVars(announcementRequest("GET", "/api/v1/admin/runtime-jobs/runs/"+run.ID+"/attempts", token, ""), map[string]string{"id": run.ID})
	attemptResponse := httptest.NewRecorder()
	h.AdminRuntimeAttemptsHandler(attemptResponse, attemptRequest)
	if attemptResponse.Code != 200 || !strings.Contains(attemptResponse.Body.String(), `"attempt_number":1`) || !strings.Contains(attemptResponse.Body.String(), `"state":"cancel_requested"`) || strings.Contains(attemptResponse.Body.String(), claim.Token) {
		t.Fatal(attemptResponse.Code, attemptResponse.Body.String())
	}
	if err := store.Finish(context.Background(), claim, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
}
