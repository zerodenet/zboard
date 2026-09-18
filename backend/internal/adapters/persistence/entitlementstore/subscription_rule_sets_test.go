package entitlementstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type ruleContentStub struct{ values map[string][]byte }

func (s *ruleContentStub) Read(_ context.Context, tag string) ([]byte, bool, error) {
	value, ok := s.values[tag]
	return append([]byte(nil), value...), ok, nil
}
func (s *ruleContentStub) Write(_ context.Context, tag string, value []byte) error {
	s.values[tag] = append([]byte(nil), value...)
	return nil
}
func (s *ruleContentStub) RemoveSource(_ context.Context, tag string) error {
	delete(s.values, tag)
	return nil
}
func (s *ruleContentStub) RemoveAll(ctx context.Context, tag string) error {
	return s.RemoveSource(ctx, tag)
}

func subscriptionRuleSetFixture(t *testing.T) (*SubscriptionRuleSets, model.User) {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "rules-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	return &SubscriptionRuleSets{DB: db, Content: &ruleContentStub{values: map[string][]byte{}}}, admin
}

func TestSubscriptionRuleSetSaveRestoresContentWhenAuditRollsBack(t *testing.T) {
	store, admin := subscriptionRuleSetFixture(t)
	ctx := context.Background()
	created, _, err := store.SaveRuleSet(ctx, admin.ID, entitlements.SubscriptionRuleSet{Name: "Rules", Renderer: managedRuleRenderer, Tag: "rules", Format: "zero_rule_ir", Interval: 3600, IsActive: true}, nil, []byte("old"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec(`CREATE TRIGGER fail_rule_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'subscription_rule_set.update' BEGIN SELECT RAISE(ABORT, 'audit failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	expected := created.Revision
	created.Name = "Changed"
	_, _, err = store.SaveRuleSet(ctx, admin.ID, created, &expected, []byte("new"), true)
	if err == nil {
		t.Fatal("audit failure accepted")
	}
	content, found, err := store.Content.Read(ctx, created.Tag)
	if err != nil || !found || string(content) != "old" {
		t.Fatalf("content rollback = %q found=%t err=%v", content, found, err)
	}
	var persisted model.SubscriptionRuleSet
	if err := store.DB.First(&persisted, created.ID).Error; err != nil || persisted.Name != "Rules" || persisted.Revision != 1 {
		t.Fatalf("database rollback = %+v err=%v", persisted, err)
	}
}

func TestSubscriptionRuleSetGuardsPermissionRevisionAndReferences(t *testing.T) {
	store, admin := subscriptionRuleSetFixture(t)
	ctx := context.Background()
	created, _, err := store.SaveRuleSet(ctx, admin.ID, entitlements.SubscriptionRuleSet{Name: "Rules", Renderer: managedRuleRenderer, Tag: "rules", Format: "zero_rule_ir", Interval: 3600, IsActive: true}, nil, []byte("old"), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveRuleSet(ctx, 0, created, nil, nil, false); !errors.Is(err, entitlements.ErrAdministrativeRead) {
		t.Fatalf("permission error = %v", err)
	}
	stale := uint64(99)
	if _, _, err := store.SaveRuleSet(ctx, admin.ID, created, &stale, []byte("new"), true); !errors.Is(err, entitlements.ErrRuleSetConflict) {
		t.Fatalf("revision error = %v", err)
	}
	template := model.SubscriptionTemplate{Name: "Zero", Slug: "zero-template", Renderer: "znet-sink", Customization: []byte(`{}`), IsActive: true, Revision: 1}
	if err := store.DB.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.SubscriptionTemplateRuleSetBinding{SubscriptionTemplateID: template.ID, SubscriptionRuleSetID: created.ID, Action: "reject", Position: 0}).Error; err != nil {
		t.Fatal(err)
	}
	created.Format = managedRuleClientFormat
	expected := created.Revision
	if _, _, err := store.SaveRuleSet(ctx, admin.ID, created, &expected, []byte("client"), true); !errors.Is(err, entitlements.ErrRuleSetClientCompatibility) {
		t.Fatalf("compatibility error = %v", err)
	}
	deleted, err := store.DeleteRuleSet(ctx, admin.ID, created.ID)
	if !errors.Is(err, entitlements.ErrRuleSetInUse) || deleted.UsageCount != 1 {
		t.Fatalf("delete reference guard = %+v err=%v", deleted, err)
	}
}

func TestSubscriptionRuleSetDirectoryRechecksAdministratorAndProjectsUsage(t *testing.T) {
	store, admin := subscriptionRuleSetFixture(t)
	ctx := context.Background()
	created, _, err := store.SaveRuleSet(ctx, admin.ID, entitlements.SubscriptionRuleSet{Name: "Directory Rules", Renderer: managedRuleRenderer, Tag: "directory-rules", Format: "zero_rule_ir", Interval: 3600, IsActive: true}, nil, []byte("rules"), true)
	if err != nil {
		t.Fatal(err)
	}
	template := model.SubscriptionTemplate{Name: "Directory Template", Slug: "directory-template", Renderer: "clash", Customization: []byte(`{}`), IsActive: true, Revision: 1}
	if err := store.DB.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.SubscriptionTemplateRuleSetBinding{SubscriptionTemplateID: template.ID, SubscriptionRuleSetID: created.ID, Action: "proxy", Position: 0}).Error; err != nil {
		t.Fatal(err)
	}
	page, err := store.ListRuleSets(ctx, admin.ID, entitlements.SubscriptionRuleSetQuery{Keyword: "Directory", Limit: 20})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].UsageCount != 1 {
		t.Fatalf("directory page = %+v err=%v", page, err)
	}
	detail, _, err := store.GetRuleSet(ctx, admin.ID, created.ID)
	if err != nil || detail.UsageCount != 1 {
		t.Fatalf("directory detail = %+v err=%v", detail, err)
	}
	if err := store.DB.Model(&model.User{}).Where("id = ?", admin.ID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListRuleSets(ctx, admin.ID, entitlements.SubscriptionRuleSetQuery{Limit: 20}); !errors.Is(err, entitlements.ErrAdministrativeRead) {
		t.Fatalf("revoked directory error = %v", err)
	}
}
