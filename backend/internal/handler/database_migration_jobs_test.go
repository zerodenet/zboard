package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestMySQLDatabaseMigrationUsesSharedRuntimeAndRetainsCompletedRun(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	pool, _ := h.db.DB()
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	if err := h.ReconcileSystemConfigDefaults(); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "migration-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: admin.ID, Email: admin.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.services.RegisterDatabaseMigrationJobs(h.credentialCipher); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(t.TempDir(), "target.db")
	payload, _ := json.Marshal(databaseMigrationRequest{TargetDriver: "sqlite", TargetDataSource: targetPath, Confirm: true})
	w := httptest.NewRecorder()
	h.AdminDatabaseMigrationStartHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/database/migrate", token, string(payload)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("migration not accepted: %d %s", w.Code, w.Body.String())
	}
	var run jobstore.Record
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := h.db.Where("handler = ?", "database_migration").First(&run).Error; err != nil {
			t.Fatal(err)
		}
		if run.State == string(jobs.Succeeded) {
			break
		}
		if run.State == string(jobs.Failed) || run.State == string(jobs.Unknown) {
			t.Fatalf("migration failed: %s", run.State)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if run.State != string(jobs.Succeeded) || run.Resource != jobs.MaintenanceResource {
		t.Fatal("shared execution did not complete", run.State)
	}
	target, err := datastore.OpenWithDriver("sqlite", targetPath)
	if err != nil {
		t.Fatal(err)
	}
	targetPool, _ := target.DB()
	defer targetPool.Close()
	var copied jobstore.Record
	if err := target.First(&copied, "id = ?", run.ID).Error; err != nil || copied.State != string(jobs.Succeeded) {
		t.Fatal("target lost execution completion", copied.State, err)
	}
	var attempt jobstore.Attempt
	if err := target.First(&attempt, "run_id = ?", run.ID).Error; err != nil || attempt.State != string(jobs.Succeeded) {
		t.Fatal("target attempt unfinished", err)
	}
	var task model.Task
	if err := target.Where("type = ?", taskTypeDatabaseMigration).First(&task).Error; err != nil || task.Status != taskStatusCompleted || task.Content != "{}" {
		t.Fatal("target task not completed", err)
	}
	if state, err := h.loadMaintenanceState(true); err != nil || !state.Enabled || !state.MigrationCutoverPending {
		t.Fatal("maintenance cleared before cutover", state, err)
	}
	status, err := h.services.RuntimeExecutionStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshots := status.Jobs
	found := false
	for _, snapshot := range snapshots {
		if snapshot.ID == "database_migration" {
			found = snapshot.Runs == 1 && snapshot.LastResult == "succeeded"
		}
	}
	if !found {
		t.Fatal("migration missing from global task catalog")
	}
}
func TestMigrationReviewUpdatesTaskAndFencesLateCompletion(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	var admin model.User
	if err := h.db.First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: taskTypeDatabaseMigration, Scope: "{}", Content: "{}", Status: taskStatusRunning, Total: 5}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]interface{}{"revision": "1", "task_id": task.ID})
	run, err := jobstore.New(h.db).Submit(context.Background(), jobs.Submission{Owner: "system", Handler: "database_migration", Key: "database_migration:1", Resource: jobs.MaintenanceResource, Payload: string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", jobs.Unknown).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.services.JobReviews.Resolve(context.Background(), jobs.Reviewer{AccountID: admin.ID}, jobs.Review{RunID: run.ID, Outcome: jobs.Succeeded, Reason: "verified destination snapshot"}); err != nil {
		t.Fatal(err)
	}
	if err := (platformstore.MigrationExecution{DB: h.db}).Finish(context.Background(), task.ID, errors.New("late worker")); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("late worker could overwrite verified result", err)
	}
	if err := h.db.First(&task, task.ID).Error; err != nil || task.Status != taskStatusCompleted || task.Current != task.Total {
		t.Fatal("review did not finalize task", err)
	}
}
func TestRestartKeepsQueuedSharedMigrationIntent(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	task := model.Task{Type: taskTypeDatabaseMigration, Scope: "{}", Content: "{}", Status: taskStatusPending}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := jobstore.New(h.db).Submit(context.Background(), jobs.Submission{Owner: "system", Handler: "database_migration", Key: "database_migration:1", Resource: jobs.MaintenanceResource, Payload: `{"revision":"1","task_id":1}`}); err != nil {
		t.Fatal(err)
	}
	if err := h.services.RecoverLegacyDatabaseMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.db.First(&task, task.ID).Error; err != nil || task.Status != taskStatusPending {
		t.Fatal("restart discarded persistent queued migration", err)
	}
}

func TestQueuedMigrationRechecksAdministratorBeforeCopy(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	var actor model.User
	if err := h.db.First(&actor).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&actor).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: taskTypeDatabaseMigration, Scope: "{}", Content: `{"ciphertext":"zboard:v1:not-opened"}`, Status: taskStatusPending}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]interface{}{"revision": "1", "task_id": task.ID, "actor_id": actor.ID})
	store := jobstore.New(h.db)
	run, err := store.Submit(context.Background(), jobs.Submission{Owner: "system", Handler: "database_migration", Key: "database_migration:1", Resource: jobs.MaintenanceResource, Payload: string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	err = (jobs.Executor{Store: store, Worker: "test", Handlers: map[string]jobs.Handler{"database_migration": h.services.MigrationExecution(h.credentialCipher).Execute}, Timeout: time.Minute}).RunOne(context.Background())
	if !errors.Is(err, jobs.ErrPermission) {
		t.Fatal("revoked administrator reached copy", err)
	}
	var saved jobstore.Record
	if err := h.db.First(&saved, "id = ?", run.ID).Error; err != nil || saved.State != string(jobs.Failed) {
		t.Fatal("denied execution not recorded", err)
	}
	if err := h.db.First(&task, task.ID).Error; err != nil || task.Status != taskStatusFailed || task.Content != "{}" {
		t.Fatal("denied task not cleaned", err)
	}
}

func TestUnknownMigrationCannotDisableMaintenanceBeforeReview(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.ReconcileSystemConfigDefaults(); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := h.db.First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: admin.ID, Email: admin.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: taskTypeDatabaseMigration, Scope: "{}", Content: "{}", Status: taskStatusFailed}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	run, err := jobstore.New(h.db).Submit(context.Background(), jobs.Submission{Owner: "system", Handler: "database_migration", Key: "database_migration:1", Resource: jobs.MaintenanceResource, Payload: `{"revision":"1","task_id":1}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", jobs.Unknown).Error; err != nil {
		t.Fatal(err)
	}
	if state, err := h.loadMaintenanceState(true); err != nil || !state.Enabled || !state.MigrationInProgress {
		t.Fatal("unknown migration hidden by failed task", state, err)
	}
	var configs []model.SystemConfig
	if err := h.db.Find(&configs).Error; err != nil {
		t.Fatal(err)
	}
	revisions := map[string]uint64{}
	for _, config := range configs {
		revisions[config.ConfigKey] = config.Revision
	}
	body, _ := json.Marshal(maintenanceUpdateRequest{Enabled: false, Title: "维护", Message: "等待核验", ExpectedRevisions: revisions})
	w := httptest.NewRecorder()
	h.AdminMaintenanceUpdateHandler(w, announcementRequest(http.MethodPut, "/api/v1/admin/maintenance", token, string(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatal("unknown migration allowed maintenance disable", w.Code, w.Body.String())
	}
	if err := h.services.JobReviews.Resolve(context.Background(), jobs.Reviewer{AccountID: admin.ID}, jobs.Review{RunID: run.ID, Outcome: jobs.Failed, Reason: "verified destination was not completed"}); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.AdminMaintenanceUpdateHandler(w, announcementRequest(http.MethodPut, "/api/v1/admin/maintenance", token, string(body)))
	if w.Code != http.StatusOK {
		t.Fatal("reviewed failure still blocks maintenance disable", w.Code, w.Body.String())
	}
}
