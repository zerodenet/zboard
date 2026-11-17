package jobstore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func taskIdentityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "task-identities.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestBatchItemsExposeBusinessNamesAndKeepMissingTargets(t *testing.T) {
	db := taskIdentityDB(t)
	ctx := context.Background()
	admin := model.User{ID: 1, Email: "admin@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	user := model.User{ID: 4, Email: "customer@example.test", AccountName: "login-name", AdminNote: "张先生", Password: "unused", Status: "active"}
	for _, row := range []*model.User{&admin, &user} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	node := model.Node{ID: 4, Name: "香港入口", Address: "192.0.2.4"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "email", Scope: "{}", Content: "{}", IdempotencyKey: "task-names", MaxAttempts: 3}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.TaskItem{{TaskID: task.ID, TargetType: "user", TargetID: "4", Payload: "{}"}, {TaskID: task.ID, TargetType: "node", TargetID: "4", Payload: "{}"}, {TaskID: task.ID, TargetType: "user", TargetID: "999", Payload: "{}"}}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	page, err := (BatchQueries{DB: db}).Items(ctx, admin.ID, task.ID, jobs.BatchFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Items[0].TargetName != user.Email || page.Items[0].TargetDescription != "张先生" || page.Items[1].TargetName != node.Name || !page.Items[2].TargetMissing || page.Items[2].TargetName != "用户已删除" {
		t.Fatalf("identities: %+v", page)
	}
	detail, err := (BatchQueries{DB: db}).Detail(ctx, admin.ID, task.ID, true)
	if err != nil || detail.Items[0].TargetName != user.Email {
		t.Fatal("detail did not use the same identity projection", err)
	}
	if _, err = (BatchQueries{DB: db}).Items(ctx, user.ID, task.ID, jobs.BatchFilter{Limit: 50}); !errors.Is(err, jobs.ErrBatchPermission) {
		t.Fatal("nonadmin identity exposure", err)
	}
	if err := db.Unscoped().Delete(&model.User{}, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	page, err = (BatchQueries{DB: db}).Items(ctx, admin.ID, task.ID, jobs.BatchFilter{Limit: 50})
	if err != nil || page.Total != 3 || !page.Items[0].TargetMissing {
		t.Fatal("deleted target disappeared from history", err)
	}
}
func TestBatchTargetNameLookupIsBatchedAndChunksDoNotIntersect(t *testing.T) {
	db := taskIdentityDB(t)
	users := make([]model.User, 501)
	items := make([]jobs.BatchItemView, 501)
	for i := range users {
		users[i] = model.User{ID: uint(i + 1), Email: fmt.Sprintf("user%d@example.test", i+1), Password: "unused", Status: "active"}
		items[i] = jobs.BatchItemView{BatchItem: jobs.BatchItem{TargetType: "user", TargetID: fmt.Sprint(i + 1)}}
	}
	if err := db.CreateInBatches(users, 100).Error; err != nil {
		t.Fatal(err)
	}
	reads := 0
	if err := db.Callback().Row().Before("gorm:row").Register("test:target-name-batching", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := batchTargetNames(db, items); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatalf("identity reads=%d, want 2 for 501 users", reads)
	}
	if items[0].TargetName != users[0].Email || items[500].TargetName != users[500].Email {
		t.Fatal("later query chunks lost their labels")
	}
}
