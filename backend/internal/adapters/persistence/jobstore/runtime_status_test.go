package jobstore

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestNativeExecutionStatusesUseGroupedProjection(t *testing.T) {
	store, _ := fixture(t, 4)
	now := time.Now().UTC().Truncate(time.Millisecond)
	store.now = func() time.Time { return now }
	oldFinished, newFinished := now.Add(-2*time.Minute), now.Add(-time.Minute)
	future, expired := now.Add(time.Minute), now.Add(-time.Minute)
	rows := []Record{
		{ID: "00000000-0000-0000-0000-000000000001", Owner: "system", Key: "cert-old", Handler: "certificate_operation", State: string(jobs.Succeeded), Token: "one", FinishedAt: &oldFinished, CreatedAt: oldFinished, NotBefore: oldFinished},
		{ID: "00000000-0000-0000-0000-000000000002", Owner: "system", Key: "cert-new", Handler: "certificate_operation", State: string(jobs.Failed), Token: "two", FinishedAt: &newFinished, CreatedAt: newFinished, NotBefore: newFinished},
		{ID: "00000000-0000-0000-0000-000000000003", Owner: "system", Key: "dns", Handler: "dns_operation", State: string(jobs.Queued), CreatedAt: now, NotBefore: now},
		{ID: "00000000-0000-0000-0000-000000000004", Owner: "system", Key: "reconcile", Handler: "dns_reconcile", State: string(jobs.Running), Token: "four", ExpiresAt: &future, CreatedAt: now, NotBefore: now},
		{ID: "00000000-0000-0000-0000-000000000005", Owner: "system", Key: "migration", Handler: "database_migration", State: string(jobs.Running), Token: "five", ExpiresAt: &expired, CreatedAt: now, NotBefore: now},
		{ID: "00000000-0000-0000-0000-000000000006", Owner: "plugin:x", Key: "foreign", Handler: "certificate_operation", State: string(jobs.Failed), Token: "six", FinishedAt: &newFinished, CreatedAt: now, NotBefore: now},
	}
	if err := store.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	statuses, err := store.NativeExecutionStatuses(context.Background(), []string{"certificate_operation", "dns_operation", "dns_reconcile", "database_migration"})
	if err != nil {
		t.Fatal(err)
	}
	byHandler := map[string]NativeExecutionStatus{}
	for _, status := range statuses {
		byHandler[status.Handler] = status
	}
	certificate := byHandler["certificate_operation"]
	if certificate.Runs != 2 || certificate.Failures != 1 || certificate.LastState != string(jobs.Failed) || certificate.LastFinishedAt == nil || !certificate.LastFinishedAt.Equal(newFinished) {
		t.Fatalf("certificate status = %#v", certificate)
	}
	if dns := byHandler["dns_operation"]; dns.Pending != 1 || dns.Runs != 0 {
		t.Fatalf("dns status = %#v", dns)
	}
	if reconcile := byHandler["dns_reconcile"]; reconcile.Running != 1 || reconcile.Unknown != 0 {
		t.Fatalf("reconcile status = %#v", reconcile)
	}
	if migration := byHandler["database_migration"]; migration.Running != 0 || migration.Unknown != 1 || migration.Runs != 1 {
		t.Fatalf("migration status = %#v", migration)
	}
}
