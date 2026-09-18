package networkstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestLegacyNativeRecoveryIsIdempotentAndNeverClaimsOldWork(t *testing.T) {
	db, _ := administrationFixture(t)
	s := jobstore.New(db)
	seedLegacyResources(t, db)
	ctx := context.Background()
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	cert := model.CertificateOperation{ManagedCertificateID: 9, NodeID: 1, OperationType: "issue", Status: "running", Phase: "requesting", CreatedAt: old}
	dns := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7, OperationType: "sync", Status: "running", Phase: "queued", CreatedAt: old}
	if err := db.Create(&cert).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&dns).Error; err != nil {
		t.Fatal(err)
	}
	count, err := (Recovery{DB: db}).RecoverLegacyNative(ctx)
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	count, err = (Recovery{DB: db}).RecoverLegacyNative(ctx)
	if err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var records []Record
	if err := db.Order("handler").Find(&records).Error; err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	for _, record := range records {
		if record.State != string(jobs.Unknown) || !record.CreatedAt.Equal(old) || record.Worker != "" || record.Token != "" {
			t.Fatal("invented execution", record)
		}
	}
	var attempts int64
	if err := db.Model(&Attempt{}).Count(&attempts).Error; err != nil || attempts != 0 {
		t.Fatal("invented attempt", attempts, err)
	}
	if _, err := s.Claim(ctx, "worker", []string{"certificate_operation", "dns_operation"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("old work claimed", err)
	}
	submit(t, s, "new-same-resource", "new-task", "certificate:9")
	if _, err := s.Claim(ctx, "worker", []string{"new-task"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("unknown resource not fenced", err)
	}
	// An already accepted Run must retain its original state and identity.
	next := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 8, OperationType: "sync", Status: "running", Phase: "queued"}
	if err := db.Create(&next).Error; err != nil {
		t.Fatal(err)
	}
	existing := submit(t, s, fmt.Sprintf("dns_operation:%d", next.ID), "dns_operation", "dns:8")
	count, err = (Recovery{DB: db}).RecoverLegacyNative(ctx)
	if err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var after Record
	if err := db.First(&after, "id = ?", existing.ID).Error; err != nil || after.State != string(jobs.Queued) {
		t.Fatal("existing run overwritten", after.State, err)
	}
}

func TestLegacyImportAndReviewKeepAuditAndDomainOutcomeConsistent(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	ctx := context.Background()
	op := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7, OperationType: "sync", Status: "running", Phase: "applying_record"}
	if err := db.Create(&op).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:reject_import", func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "job.legacy.import" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Recovery{DB: db}).RecoverLegacyNative(ctx); err == nil {
		t.Fatal("failed audit committed")
	}
	var count int64
	if err := db.Model(&Record{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("Run escaped rollback", count, err)
	}
	if err := db.Callback().Create().Remove("test:reject_import"); err != nil {
		t.Fatal(err)
	}
	if _, err := (Recovery{DB: db}).RecoverLegacyNative(ctx); err != nil {
		t.Fatal(err)
	}
	var record Record
	if err := db.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "reviewer@example.test", Password: "hash", Status: "active", IsAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	service := jobs.ReviewService{Repository: JobReviews{DB: db}}
	in := jobs.Review{RunID: record.ID, Outcome: jobs.Succeeded, Reason: "verified resource"}
	if err := service.Resolve(ctx, jobs.Reviewer{AccountID: admin.ID}, in); !errors.Is(err, jobs.ErrOutcomeUnverified) {
		t.Fatal(err)
	}
	if err := db.First(&record, "id = ?", record.ID).Error; err != nil || record.State != string(jobs.Unknown) {
		t.Fatal("review escaped rollback", err)
	}
	if err := db.Model(&op).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Resolve(ctx, jobs.Reviewer{AccountID: admin.ID}, in); !errors.Is(err, jobs.ErrOutcomeUnverified) {
		t.Fatal("contradictory review", err)
	}
	in.Outcome = jobs.Failed
	if err := service.Resolve(ctx, jobs.Reviewer{AccountID: admin.ID}, in); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "job.resolve").Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func seedLegacyResources(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, row := range []any{
		&model.User{ID: 1, Email: "owner@example.test", Password: "hash", Status: "active"},
		&model.ProviderAccount{ID: 1, ProviderKey: "test", Name: "test", Capabilities: "[]", CredentialCiphertext: "unused", CreatedBy: 1},
		&model.Node{ID: 1, Name: "test", Config: "{}"},
		&model.ManagedCertificate{ID: 9, NodeID: 1, Name: "test", Domains: `["example.test"]`, Status: "issuing"},
		&model.ManagedDNSRecord{ID: 7, ProviderAccountID: 1, NodeID: 1, DomainName: "a.example.test", RecordType: "A", RecordValue: "192.0.2.1", Status: "syncing", DesiredHash: "test", CreatedBy: 1},
		&model.ManagedDNSRecord{ID: 8, ProviderAccountID: 1, NodeID: 1, DomainName: "b.example.test", RecordType: "A", RecordValue: "192.0.2.1", Status: "syncing", DesiredHash: "test", CreatedBy: 1},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

type Record = jobstore.Record
type Attempt = jobstore.Attempt

func submit(t *testing.T, s *jobstore.Store, key, handler, resource string) jobs.Run {
	t.Helper()
	run, err := s.Submit(context.Background(), jobs.Submission{Owner: "system", Key: key, Handler: handler, Resource: resource, Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	return run
}
