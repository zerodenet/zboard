package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRuntimeViewsReadOnlyRequestedPartitions(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, _ := h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	var expensiveReads atomic.Int64
	// A stalled business/execution query must never be started by the runtime view.
	observe := func(db *gorm.DB) {
		sql := strings.ToLower(db.Statement.SQL.String())
		if strings.Contains(sql, "tasks") || strings.Contains(sql, "job_execution") || strings.Contains(sql, "registration_events") {
			expensiveReads.Add(1)
			db.AddError(assertRuntimePartitionError{})
		}
	}
	const name = "test:runtime_partitions"
	h.db.Callback().Query().Before("gorm:query").Register(name, observe)
	h.db.Callback().Row().Before("gorm:row").Register(name, observe)
	h.db.Callback().Raw().Before("gorm:raw").Register(name, observe)
	defer h.db.Callback().Query().Remove(name)
	defer h.db.Callback().Row().Remove(name)
	defer h.db.Callback().Raw().Remove(name)
	for _, view := range []string{"runtime", "plugins"} {
		rec := httptest.NewRecorder()
		h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs?view="+view, token, ""))
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		var body struct{ Data map[string]json.RawMessage }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"jobs", "queues", "execution_queue", "registered_jobs"} {
			if _, ok := body.Data[key]; ok {
				t.Fatalf("%s included unread field %s", view, key)
			}
		}
		var issues []runtimeStatusIssue
		_ = json.Unmarshal(body.Data["issues"], &issues)
		if len(issues) != 0 {
			t.Fatalf("%s: %+v", view, issues)
		}
	}
	if expensiveReads.Load() != 0 {
		t.Fatalf("runtime/plugins started %d unrelated reads", expensiveReads.Load())
	}
}

type assertRuntimePartitionError struct{}

func (assertRuntimePartitionError) Error() string { return "unrelated partition must not run" }

func TestRuntimeViewRejectsUnknownViewAndStillChecksAdmin(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	rec := httptest.NewRecorder()
	h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs?view=runtime", token, ""))
	if rec.Code != 403 {
		t.Fatal(rec.Code)
	}
	h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true)
	token, _, _ = h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	rec = httptest.NewRecorder()
	h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs?view=unknown", token, ""))
	if rec.Code != 400 {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestFocusedRuntimeValuesMatchLegacySnapshot(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true)
	token, _, _ := h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	read := func(view string) map[string]json.RawMessage {
		t.Helper()
		rec := httptest.NewRecorder()
		h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs?view="+view, token, ""))
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		var body struct{ Data map[string]json.RawMessage }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Data
	}
	full := read("")
	for view, fields := range map[string][]string{
		"execution": {"jobs", "execution_queue", "registered_jobs", "execution_concurrency", "external_execution_concurrency"},
		"queues":    {"queues"}, "plugins": {"plugin_host", "plugin_tasks"},
	} {
		focused := read(view)
		for _, field := range fields {
			if string(focused[field]) != string(full[field]) {
				t.Fatalf("%s changed %s: %s != %s", view, field, focused[field], full[field])
			}
		}
		var durations map[string]int64
		if err := json.Unmarshal(focused["read_duration_ms"], &durations); err != nil {
			t.Fatal(err)
		}
		expected := 1
		if view == "queues" {
			expected = 2
		}
		if len(durations) != expected {
			t.Fatalf("%s timings include unrelated sections: %+v", view, durations)
		}
	}
	h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", false)
	rec := httptest.NewRecorder()
	h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs?view=runtime", token, ""))
	if rec.Code != 403 {
		t.Fatalf("revoked admin read succeeded: %d", rec.Code)
	}
}
