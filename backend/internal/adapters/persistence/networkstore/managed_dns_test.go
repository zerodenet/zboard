package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type managedDNSCipher struct{}

func (managedDNSCipher) Encrypt(value string) (string, error) { return "encrypted:" + value, nil }
func (managedDNSCipher) Decrypt(value string) (string, error) {
	if value != "encrypted:provider-token" {
		return "", errors.New("unexpected ciphertext")
	}
	return "provider-token", nil
}

type managedDNSProvider struct{ t *testing.T }

func (p managedDNSProvider) ApplyManagedDNS(_ context.Context, request network.ManagedDNSProviderRequest, progress func(string) error) (network.ManagedDNSProviderResult, error) {
	p.t.Helper()
	if request.ProviderKey != "cloudflare" || request.Credential != "provider-token" || request.Takeover {
		p.t.Fatalf("request=%+v", request)
	}
	if err := progress("inspecting_record"); err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	if err := progress("applying_record"); err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	record := request.Record
	return network.ManagedDNSProviderResult{ZoneID: "zone-1", RecordID: "record-1", RecordType: record.RecordType, Name: record.DomainName, Value: record.RecordValue, TTL: record.TTL, Proxied: record.Proxied}, nil
}

type managedDNSObserver struct{}

func (managedDNSObserver) ManagedDNSResolves(context.Context, network.ManagedDNSRecord) bool {
	return true
}

func managedDNSFixture(t *testing.T) (*gorm.DB, network.ManagedDNS) {
	t.Helper()
	db, _ := administrationFixture(t)
	for _, row := range []any{
		&model.User{ID: 1, Email: "dns-admin@example.test", Password: "unused", Status: "active", IsAdmin: true},
		&model.Node{ID: 1, Name: "dns-node", Address: "203.0.113.10", SSHHost: "2001:db8::10", Config: "{}", LifecycleStatus: "active", IsEnabled: true},
		&model.ProviderAccount{ID: 1, ProviderKey: "cloudflare", Name: "dns-provider", Capabilities: `["dns.records"]`, CredentialCiphertext: "encrypted:provider-token", CredentialPrefix: "prov…oken", Status: "active", Revision: 1, CreatedBy: 1},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, network.ManagedDNS{Repository: ManagedDNS{DB: db}, Cipher: managedDNSCipher{}, Provider: managedDNSProvider{t: t}, Observer: managedDNSObserver{}}
}

func TestManagedDNSLifecycleOwnsRecordsRunsAndExecutionOutcome(t *testing.T) {
	db, service := managedDNSFixture(t)
	ctx := context.Background()
	records, operations, err := service.Create(ctx, 1, network.ManagedDNSWrite{
		ProviderAccountID: 1, NodeID: 1, DomainName: " Edge.Example.Test. ", TakeoverExisting: false,
		Records: []network.ManagedDNSRecordInput{{RecordType: "A"}, {RecordType: "AAAA"}},
	})
	if err != nil || len(records) != 2 || len(operations) != 2 {
		t.Fatalf("records=%+v operations=%+v error=%v", records, operations, err)
	}
	if records[0].DomainName != "edge.example.test" || records[0].RecordValue != "203.0.113.10" || records[1].RecordValue != "2001:db8::10" {
		t.Fatalf("normalized records=%+v", records)
	}
	var runs int64
	if err := db.Model(&jobstore.Record{}).Where("handler = ?", "dns_operation").Count(&runs).Error; err != nil || runs != 2 {
		t.Fatalf("runs=%d error=%v", runs, err)
	}
	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "dns_record.create").Count(&audits).Error; err != nil || audits != 2 {
		t.Fatalf("audits=%d error=%v", audits, err)
	}
	page, err := service.List(ctx, 1, network.ManagedDNSListQuery{Search: "edge", Offset: 0, Limit: 20})
	if err != nil || page.Total != 2 || len(page.Records) != 2 || page.Records[0].ProviderName != "dns-provider" || page.Records[0].NodeName != "dns-node" || page.Records[0].LatestOperation == nil {
		t.Fatalf("page=%+v error=%v", page, err)
	}
	if err := service.Execute(ctx, operations[0].ID); err != nil {
		t.Fatal(err)
	}
	var completed model.ProviderOperation
	if err := db.First(&completed, operations[0].ID).Error; err != nil || completed.Status != "succeeded" || completed.Phase != "completed" {
		t.Fatalf("operation=%+v error=%v", completed, err)
	}
	var resolved model.ManagedDNSRecord
	if err := db.First(&resolved, records[0].ID).Error; err != nil || resolved.Status != network.ManagedDNSActive || !resolved.PublicResolved || resolved.ProviderRecordID != "record-1" {
		t.Fatalf("record=%+v error=%v", resolved, err)
	}
}

func TestManagedDNSUpdateFencesRunningRevisionAuthorityAndAuditFailure(t *testing.T) {
	db, service := managedDNSFixture(t)
	ctx := context.Background()
	records, operations, err := service.Create(ctx, 1, network.ManagedDNSWrite{ProviderAccountID: 1, NodeID: 1, DomainName: "edit.example.test", RecordType: "A", RecordValue: "203.0.113.20", TTL: 120})
	if err != nil {
		t.Fatal(err)
	}
	request := network.ManagedDNSWrite{ProviderAccountID: 1, NodeID: 1, DomainName: "edit.example.test", RecordType: "A", RecordValue: "203.0.113.21", TTL: 300, ExpectedRevision: records[0].Revision}
	if _, _, err := service.Update(ctx, 1, records[0].ID, request); !errors.Is(err, network.ErrManagedDNSOperationRunning) {
		t.Fatalf("running update error=%v", err)
	}
	if err := db.Model(&model.ProviderOperation{}).Where("id = ?", operations[0].ID).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ManagedDNSRecord{}).Where("id = ?", records[0].ID).Update("status", network.ManagedDNSFailed).Error; err != nil {
		t.Fatal(err)
	}
	request.ExpectedRevision++
	if _, _, err := service.Update(ctx, 1, records[0].ID, request); !errors.Is(err, network.ErrManagedDNSRevisionConflict) {
		t.Fatalf("stale update error=%v", err)
	}
	request.ExpectedRevision--
	if err := db.Callback().Create().Before("gorm:create").Register("managed_dns:reject_audit", func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "dns_record.update" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Update(ctx, 1, records[0].ID, request); err == nil {
		t.Fatal("audit failure committed update")
	}
	if err := db.Callback().Create().Remove("managed_dns:reject_audit"); err != nil {
		t.Fatal(err)
	}
	var unchanged model.ManagedDNSRecord
	if err := db.First(&unchanged, records[0].ID).Error; err != nil || unchanged.Revision != records[0].Revision || unchanged.RecordValue != records[0].RecordValue {
		t.Fatalf("record=%+v error=%v", unchanged, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(ctx, 1, records[0].ID, false); !errors.Is(err, network.ErrManagedDNSPermission) {
		t.Fatalf("revoked start error=%v", err)
	}
}

func TestManagedDNSObservationOnlyAdvancesSelectedUnresolvedRows(t *testing.T) {
	db, service := managedDNSFixture(t)
	checked := time.Now().UTC().Add(-time.Hour)
	rows := []model.ManagedDNSRecord{
		{ProviderAccountID: 1, NodeID: 1, DomainName: "one.example.test", RecordType: "A", RecordValue: "203.0.113.1", Status: network.ManagedDNSActive, DesiredHash: "one", LastSyncedAt: &checked, LastPublicCheckAt: &checked, Revision: 1, CreatedBy: 1},
		{ProviderAccountID: 1, NodeID: 1, DomainName: "done.example.test", RecordType: "A", RecordValue: "203.0.113.2", Status: network.ManagedDNSActive, DesiredHash: "done", LastSyncedAt: &checked, LastPublicCheckAt: &checked, PublicResolved: true, Revision: 1, CreatedBy: 1},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.ObservePublic(context.Background(), time.Now().UTC(), 15*time.Second, time.Second, 2); err != nil {
		t.Fatal(err)
	}
	var first, second model.ManagedDNSRecord
	if err := db.First(&first, rows[0].ID).Error; err != nil || !first.PublicResolved || !first.LastPublicCheckAt.After(checked) {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	if err := db.First(&second, rows[1].ID).Error; err != nil || !second.LastPublicCheckAt.Equal(checked) {
		t.Fatalf("second=%+v error=%v", second, err)
	}
}
