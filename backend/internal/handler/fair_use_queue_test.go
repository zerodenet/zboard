package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestFairUseManualEvaluationUsesGlobalQueueAndRechecksActor(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	// Keep the bootstrap gate closed so the real pool cannot race the explicit claim below.
	h.services.Close()
	h.services = application.New(h.db, "0123456789abcdef0123456789abcdef")
	actor, err := h.authFromRequest(announcementRequest("GET", "/", token, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	sub := model.Subscription{UserID: actor.UserID, Config: "{}", Status: "active", EndAt: time.Now().Add(time.Hour)}
	if err = h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	request := announcementRequest(http.MethodPost, "/api/v1/admin/subscriptions/"+strconv.FormatUint(uint64(sub.ID), 10)+"/fair-use/evaluate", token, "")
	w := httptest.NewRecorder()
	h.AdminSubscriptionFairUseEvaluateHandler(w, request)
	if w.Code != http.StatusAccepted {
		t.Fatalf("queue response: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data metering.EvaluationReceipt `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.RunID == "" || body.Data.State != "queued" {
		t.Fatalf("receipt: %+v", body)
	}
	var count int64
	if err = h.db.Model(&subscriptionFairUseState{}).Where("subscription_id = ?", sub.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("HTTP evaluated inline: %d %v", count, err)
	}
	store := jobstore.New(h.db)
	claim, err := store.Claim(context.Background(), "fair-use-test", []string{metering.EvaluationJob}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.ID != body.Data.RunID || claim.Run.Resource != "core:fair_use" {
		t.Fatalf("wrong global run: %+v", claim.Run)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if err = h.services.ExecuteFairUseJob(context.Background(), claim.Run); !errors.Is(err, metering.ErrPolicyPermission) {
		t.Fatalf("revoked queued actor: %v", err)
	}
	if err = store.Finish(context.Background(), claim, jobs.Failed); err != nil {
		t.Fatal(err)
	}
}
