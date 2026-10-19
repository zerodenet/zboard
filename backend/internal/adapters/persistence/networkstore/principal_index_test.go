package networkstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestPrincipalIndexMySQLAndSQLiteResumesBeyond4096AndBoundsHotReads(t *testing.T) {
	db, other := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	const size = 5001
	subs := make([]model.Subscription, size)
	credentials := make([]model.ProtocolCredential, size)
	for i := range subs {
		id := uint(i + 1)
		subs[i] = model.Subscription{ID: id, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "canceled", FlowTotal: 100, FlowUsed: 17, Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(-time.Minute)}
		credentials[i] = model.ProtocolCredential{ID: id, SubscriptionID: id, UserID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: fmt.Sprintf("credential-%d", id), PrincipalKey: fmt.Sprintf("subscription:%d:endpoint:1", id), Status: "revoked", ExpiresAt: now.Add(-time.Minute), CreatedAt: now, UpdatedAt: now, RetiredPrincipalHash: entitlements.RetiredPrincipalHash(1, fmt.Sprintf("retired-secret-%d", id))}
	}
	if err := db.CreateInBatches(&subs, 256).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&credentials, 256).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	completed := false
	previous := int64(0)
	for attempt := 0; attempt < 8; attempt++ {
		store := db
		if attempt%2 == 1 {
			store = other
		} // Separate connection/process-equivalent restart.
		forbidden, err := (ConnectionRevocation{DB: store}).WithRevokedPrincipal(context.Background(), 1, "retired-secret-5001", func() error { calls++; return nil })
		var indexed int64
		if countErr := db.Model(&model.ProtocolCredential{}).Where("principal_lookup_version = ?", 1).Count(&indexed).Error; countErr != nil {
			t.Fatal(countErr)
		}
		if indexed <= previous || indexed-previous > 1024 {
			t.Fatal("backfill lost progress or exceeded per-call bound", previous, indexed)
		}
		previous = indexed
		if err == nil {
			if !forbidden {
				t.Fatal("canceled identity became authorized")
			}
			completed = true
			break
		}
		if !errors.Is(err, network.ErrConnectionRevocationPending) || calls != 0 {
			t.Fatal("incomplete coverage acted on a credential", err, calls)
		}
	}
	if !completed || calls != 1 || previous != size {
		t.Fatal("lookup remained blocked beyond4096", completed, calls, previous)
	}
	var seen int64
	var lookupSQL string
	var lookupArgs []any
	if err := db.Callback().Row().After("gorm:row").Register("test_principal_query_plan", func(q *gorm.DB) {
		sql := q.Statement.SQL.String()
		if strings.HasPrefix(sql, "SELECT id FROM protocol_credentials") && strings.Contains(sql, " UNION ") {
			lookupSQL = sql
			lookupArgs = append([]any(nil), q.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test_hot_principal_rows", func(q *gorm.DB) {
		if q.RowsAffected > 0 {
			seen += q.RowsAffected
		}
	}); err != nil {
		t.Fatal(err)
	}
	forbidden, err := (ConnectionRevocation{DB: db}).WithRevokedPrincipal(context.Background(), 1, "retired-secret-5001", func() error { calls++; return nil })
	if err != nil || !forbidden || seen > 12 {
		t.Fatal("hot lookup scanned credential history", forbidden, seen, err)
	}
	if lookupSQL == "" {
		t.Fatal("actual hot lookup query not captured")
	}
	branches := 0
	if db.Dialector.Name() == "sqlite" {
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN "+lookupSQL, lookupArgs...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		for _, step := range plan {
			if strings.Contains(step.Detail, "protocol_credentials") {
				branches++
				if !strings.Contains(step.Detail, "SEARCH") || !strings.Contains(step.Detail, "INDEX") {
					t.Fatal("hot lookup scans history", step.Detail)
				}
			}
		}
	} else {
		var plan []struct {
			Table, Type string
			Key         *string
		}
		if err := db.Raw("EXPLAIN "+lookupSQL, lookupArgs...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		allowed := map[string]bool{"idx_credential_principal_digest": true, "idx_credential_secret_digest": true, "idx_credential_retired_principal": true}
		for _, step := range plan {
			if step.Table == "protocol_credentials" {
				branches++
				if step.Type == "ALL" || step.Key == nil || !allowed[*step.Key] {
					t.Fatal("hot lookup does not use a digest index", step.Type, step.Key)
				}
			}
		}
	}
	if branches != 3 {
		t.Fatal("unexpected native principal query coverage", branches)
	}
	t.Logf("retained_credentials=%d hot_returned_rows=%d digest_index_branches=%d", size, seen, branches)
	var retained model.ProtocolCredential
	if err := db.First(&retained, size).Error; err != nil || retained.Secret != "" || retained.Status != "revoked" || !retained.UpdatedAt.Equal(now) {
		t.Fatal("projection changed canonical evidence", err)
	}
	var sub model.Subscription
	if err := db.First(&sub, size).Error; err != nil || sub.FlowUsed != 17 || sub.Status != "canceled" {
		t.Fatal("projection changed business quota", err)
	}
	if err := db.Model(&model.ProtocolCredential{}).Where("id = ?", 1).Updates(map[string]any{"principal_lookup_version": 0, "retired_principal_hash": entitlements.RetiredPrincipalHash(1, "retired-secret-5001")}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = (ConnectionRevocation{DB: db}).WithRevokedPrincipal(context.Background(), 1, "retired-secret-5001", func() error { t.Fatal("ambiguous digest closed a flow"); return nil })
	if !errors.Is(err, network.ErrConnectionRevocationPending) {
		t.Fatal("ambiguous identity did not fail closed", err)
	}
}

func TestPrincipalIndexSchemaMySQLAndSQLiteRecoversProjectionLoss(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	if err := db.Create(&model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "canceled", Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	digest := entitlements.RetiredPrincipalHash(1, "historical-secret")
	if err := db.Create(&model.ProtocolCredential{ID: 1, UserID: 1, SubscriptionID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: "source", PrincipalKey: "source", Status: "revoked", ExpiresAt: now, RetiredPrincipalHash: digest, PrincipalDigest: entitlements.RetiredPrincipalHash(1, "source"), SecretPrincipalDigest: digest, PrincipalLookupVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// Recorded auxiliary loss must invalidate completion while preserving the
	// canonical stable identity and irreversible retired-secret attribution.
	if err := db.Migrator().DropIndex("protocol_credentials", "idx_credential_secret_digest"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE protocol_credentials DROP COLUMN secret_principal_digest").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var source model.ProtocolCredential
	if err := db.First(&source, 1).Error; err != nil || source.PrincipalLookupVersion != 0 || source.RetiredPrincipalHash != digest || source.PrincipalKey != "source" {
		t.Fatal("lost auxiliary column invented complete coverage or changed source", source.PrincipalLookupVersion, err)
	}
	// Same index name with the wrong recovered shape must not count as complete.
	if err := db.Migrator().DropIndex("protocol_credentials", "idx_credential_lookup_backfill"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE INDEX idx_credential_lookup_backfill ON protocol_credentials(node_id)").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	indices, err := db.Migrator().GetIndexes("protocol_credentials")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, index := range indices {
		if index.Name() == "idx_credential_lookup_backfill" {
			found = true
			if got := strings.Join(index.Columns(), ","); got != "node_id,principal_lookup_version,id" {
				t.Fatal("incorrect auxiliary index accepted", got)
			}
		}
	}
	if !found {
		t.Fatal("backfill index missing")
	}
	var node model.Node
	if err := db.First(&node, 1).Error; err != nil || node.Name != "index" {
		t.Fatal("unrelated source data changed", err)
	}
	// A partially applied migration with one column still present also repeats.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0046_principal_lookup_index.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex("protocol_credentials", "idx_credential_principal_digest"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE protocol_credentials DROP COLUMN principal_digest").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
}

func seedPrincipalIndexOwners(t *testing.T, db *gorm.DB) time.Time {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, row := range []any{
		&model.User{ID: 1, Email: "index@example.test", Password: "hash", Status: "active"}, &model.Node{ID: 1, Name: "index", Config: "{}"}, &model.NodeGroup{ID: 1, Name: "index", Code: "index"},
		&model.Plan{ID: 1, Name: "index", Slug: "index", NodeGroupID: 1}, &model.PlanSKU{ID: 1, PlanID: 1, Code: "index", Name: "index", BillingUnit: "month", BillingValue: 1, Currency: "CNY"},
		&model.ProtocolEndpoint{ID: 1, NodeID: 1, Name: "index", RuntimeKey: "index", Protocol: "shadowsocks", Address: "test", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	return now
}
