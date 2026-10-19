package networkstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestConnectionRevocationMySQLAndSQLiteFencesRenewal(t *testing.T) {
	db, other := administrationFixture(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, row := range []any{
		&model.User{ID: 1, Email: "scope@example.test", Password: "hash", Status: "active"},
		&model.Node{ID: 1, Name: "scope", Config: "{}"},
		&model.NodeGroup{ID: 1, Name: "scope", Code: "scope"},
		&model.Plan{ID: 1, Name: "scope", Slug: "scope", NodeGroupID: 1},
		&model.PlanSKU{ID: 1, PlanID: 1, Code: "scope", Name: "scope", BillingUnit: "month", BillingValue: 1, Currency: "CNY"},
		&model.ProtocolEndpoint{ID: 1, NodeID: 1, Name: "scope", RuntimeKey: "scope", Protocol: "vless", Address: "test", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]"},
		&model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "active", FlowTotal: 100, Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(-time.Minute)},
		&model.ProtocolCredential{ID: 1, SubscriptionID: 1, UserID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: "scope", PrincipalKey: "scope", Secret: "opaque", Status: "active", ExpiresAt: now.Add(time.Hour)},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	authority := ConnectionRevocation{DB: db}
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		blocked, err := authority.WithRevokedPrincipal(context.Background(), 1, "scope", func() error { close(entered); <-release; return nil })
		if err == nil && !blocked {
			err = errors.New("expired principal was allowed")
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not enter")
	}
	renewal := make(chan error, 1)
	go func() {
		renewal <- other.Model(&model.Subscription{}).Where("id = ?", 1).Update("end_at", now.Add(time.Hour)).Error
	}()
	select {
	case err := <-renewal:
		close(release)
		t.Fatal("renewal bypassed revocation fence", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-renewal; err != nil {
		t.Fatal(err)
	}
	forbidden, err := authority.WithRevokedPrincipal(context.Background(), 1, "scope", func() error { t.Fatal("closed renewed principal"); return nil })
	if err != nil || forbidden {
		t.Fatal(forbidden, err)
	}
	_, err = authority.WithRevokedPrincipal(context.Background(), 1, "unknown", func() error { t.Fatal("closed unknown principal"); return nil })
	if !errors.Is(err, network.ErrConnectionRevocationPending) {
		t.Fatal(err)
	}
}

func TestPublicationConnectionPhaseMySQLAndSQLiteSurvivesRetryAndNewGeneration(t *testing.T) {
	db, _ := administrationFixture(t)
	if err := db.Create(&model.Node{ID: 1, Name: "phase", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePublication(db, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	q := PublicationQueue{DB: db}
	now := time.Now().UTC().Truncate(time.Millisecond).Add(time.Second)
	item, ok, err := q.Claim(context.Background(), now)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	sha := strings.Repeat("a", 64)
	ok, err = q.MarkConfigurationApplied(context.Background(), item, sha)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := q.Complete(context.Background(), item, now, network.ErrConnectionRevocationPending); err != nil {
		t.Fatal(err)
	}
	retry, ok, err := q.Claim(context.Background(), now.Add(network.PublicationRecheckInterval))
	if err != nil || !ok || retry.Phase != "connections" || retry.AppliedConfigSHA256 != sha || retry.Attempts != 0 {
		t.Fatal(retry, ok, err)
	}
	if err := EnqueuePublication(db, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	ok, err = q.MarkConfigurationApplied(context.Background(), retry, sha)
	if err != nil || ok {
		t.Fatal("old worker advanced new intent", ok, err)
	}
	if err := q.Complete(context.Background(), retry, now, nil); err != nil {
		t.Fatal(err)
	}
	latest, ok, err := q.Claim(context.Background(), now.Add(time.Second))
	if err != nil || !ok || latest.Generation != item.Generation+1 || latest.Phase != "" || latest.AppliedConfigSHA256 != "" {
		t.Fatal(latest, ok, err)
	}
}

func TestPublicationConnectionVerificationAuditMySQLAndSQLiteIsAtomic(t *testing.T) {
	db, _ := administrationFixture(t)
	if err := db.Create(&model.Node{ID: 1, Name: "phase-audit", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePublication(db, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	q := PublicationQueue{DB: db}
	now := time.Now().UTC().Truncate(time.Millisecond).Add(time.Second)
	item, ok, err := q.Claim(context.Background(), now)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = q.MarkConfigurationApplied(context.Background(), item, strings.Repeat("b", 64))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	trigger := "CREATE TRIGGER reject_connection_audit BEFORE INSERT ON audit_logs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'test audit failure'"
	if db.Dialector.Name() == "sqlite" {
		trigger = "CREATE TRIGGER reject_connection_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT,'test audit failure'); END"
	}
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(context.Background(), item, now, nil); err == nil {
		t.Fatal("audit failure erased queue")
	}
	var held model.NodeConfigPublish
	if err := db.First(&held, 1).Error; err != nil || held.LeaseToken != item.LeaseToken || held.Phase != "connections" {
		t.Fatal(held, err)
	}
	if err := db.Exec("DROP TRIGGER reject_connection_audit").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := q.Complete(context.Background(), item, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "node.connection_revocation_verified").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
}

type connectionTestCipher struct{}

func (connectionTestCipher) Decrypt(value string) (string, error) {
	return strings.TrimPrefix(value, "encrypted:"), nil
}

func TestConnectionRevocationSecretAttributionRetainsNodeScope(t *testing.T) {
	authority := ConnectionRevocation{Cipher: connectionTestCipher{}}
	secret := "machine-generated-protocol-secret"
	row := model.ProtocolCredential{NodeID: 7, Secret: "encrypted:" + secret}
	if !authority.matches(7, secret, row) || authority.matches(7, "other", row) {
		t.Fatal("encrypted principal attribution")
	}
	row.Secret = ""
	row.RetiredPrincipalHash = entitlements.RetiredPrincipalHash(7, secret)
	if !authority.matches(7, secret, row) || authority.matches(8, secret, row) {
		t.Fatal("retired hash crossed node scope")
	}
}

func TestCombinedPublicationMySQLAndSQLiteDoesNotCallConnectionPendingConfigFailure(t *testing.T) {
	db, _ := administrationFixture(t)
	for _, node := range []model.Node{{ID: 1, Name: "front", Config: "{}"}, {ID: 2, Name: "landing", Config: "{}"}} {
		if err := db.Create(&node).Error; err != nil {
			t.Fatal(err)
		}
		if err := EnqueuePublication(db, node.ID, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond).Add(time.Second)
	if err := db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", 1).Updates(map[string]any{"phase": "connections", "applied_config_sha256": strings.Repeat("a", 64), "last_error": "connection verification pending", "next_attempt_at": now.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	status := func() string {
		var row struct{ Status string }
		err := db.Table("node_config_publishes AS publication").Joins("LEFT JOIN protocol_deployments AS deployment ON deployment.id = 0").Joins("LEFT JOIN node_config_publishes AS landing_publication ON landing_publication.node_id = ?", 2).Where("publication.node_id = ?", 1).Select(servicePublicationStatusSQL+" AS status", now, now, now, now).Scan(&row).Error
		if err != nil {
			t.Fatal(err)
		}
		return row.Status
	}
	if got := status(); got != "queued" {
		t.Fatal("connection verification became configuration failure", got)
	}
	if err := db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", 2).Updates(map[string]any{"last_error": "configuration rejected", "next_attempt_at": now.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "failed" {
		t.Fatal("real configuration failure was hidden", got)
	}
	if err := db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", 2).Updates(map[string]any{"phase": "connections", "applied_config_sha256": strings.Repeat("b", 64)}).Error; err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "connections_pending" {
		t.Fatal("dual connection verification misclassified", got)
	}
	if err := db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", 1).Updates(map[string]any{"phase": "", "last_error": "", "next_attempt_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "queued" {
		t.Fatal("landing connection verification became front configuration failure", got)
	}
	if err := db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", 1).Updates(map[string]any{"last_error": "configuration rejected", "next_attempt_at": now.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "failed" {
		t.Fatal("front configuration failure was hidden", got)
	}
}

func TestConnectionPrincipalMySQLAndSQLiteNeverBecomesSQLParameter(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	secret := "raw-protocol-password-for-sql-boundary-test"
	for _, row := range []any{
		&model.User{ID: 1, Email: "principal-sql@example.test", Password: "hash", Status: "active"},
		&model.Node{ID: 1, Name: "scope", Config: "{}"}, &model.NodeGroup{ID: 1, Name: "scope", Code: "scope"},
		&model.Plan{ID: 1, Name: "scope", Slug: "scope", NodeGroupID: 1}, &model.PlanSKU{ID: 1, PlanID: 1, Code: "scope", Name: "scope", BillingUnit: "month", BillingValue: 1, Currency: "CNY"},
		&model.ProtocolEndpoint{ID: 1, NodeID: 1, Name: "scope", RuntimeKey: "scope", Protocol: "shadowsocks", Address: "test", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]"},
		&model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "canceled", FlowTotal: 100, Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(-time.Minute)},
		&model.ProtocolCredential{ID: 1, SubscriptionID: 1, UserID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: "scope", PrincipalKey: "subscription:1:endpoint:1", Secret: "encrypted:" + secret, Status: "revoked", ExpiresAt: now.Add(time.Hour)},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	assertNoPrincipal := func(query *gorm.DB) {
		for _, value := range query.Statement.Vars {
			if text, ok := value.(string); ok && text == secret {
				t.Error("raw protocol principal reached SQL parameters")
			}
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register("test_no_raw_principal_sql", assertNoPrincipal); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test_no_raw_principal_row", assertNoPrincipal); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().After("gorm:raw").Register("test_no_raw_principal_exec", assertNoPrincipal); err != nil {
		t.Fatal(err)
	}

	called := false
	forbidden, err := (ConnectionRevocation{DB: db, Cipher: connectionTestCipher{}}).WithRevokedPrincipal(context.Background(), 1, secret, func() error { called = true; return nil })
	if err != nil || !forbidden || !called {
		t.Fatal("owned revoked principal not attributed", forbidden, called, err)
	}
}
