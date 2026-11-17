package networkstore

import (
	"fmt"
	"os"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

func TestNodeRemovalJobLookupIncludesEveryOwnedResource(t *testing.T) {
	nodeRemovalJobResources(t)
}

func TestMySQLNodeRemovalJobLookupIncludesEveryOwnedResource(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("requires disposable MySQL")
	}
	nodeRemovalJobResources(t)
}

func nodeRemovalJobResources(t *testing.T) {
	t.Helper()
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	resources := []string{"node:1", "node-publish:1", "dns:7", "dns:8", "dns-inspection:7", "dns-inspection:8", "certificate:9"}
	for i, resource := range append(resources, "node:999", "dns:999", "certificate:999") {
		if _, err := jobstore.New(db).Submit(t.Context(), jobs.Submission{Owner: "system", Key: fmt.Sprintf("owned-resource-%d", i), Handler: "probe", Resource: resource, Payload: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []jobs.State{jobs.Queued, jobs.Running, jobs.Unknown, jobs.Succeeded, jobs.Failed, jobs.Canceled} {
		if err := db.Model(&jobstore.Record{}).Where("handler = ?", "probe").Update("state", state).Error; err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Transaction(func(tx *gorm.DB) error {
			blockers, err := nodeRemovalBlockers(tx, 1, nil)
			count = blockers["unverified_jobs"]
			return err
		}); err != nil {
			t.Fatal(err)
		}
		want := int64(len(resources))
		if state == jobs.Succeeded || state == jobs.Failed || state == jobs.Canceled {
			want = 0
		}
		if count != want {
			t.Fatalf("state=%s owned blockers=%d want=%d", state, count, want)
		}
	}
}
