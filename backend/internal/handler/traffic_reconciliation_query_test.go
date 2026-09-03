package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestTrafficReconciliationMySQLScopesDriveIndexedJoin(t *testing.T) {
	f := newTrafficReadFixture(t)
	// Borrow the fixture pool only to build MySQL SQL. DryRun never executes
	// this dialect against SQLite and needs no local Docker/MySQL server.
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: f.h.db.ConnPool, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db = db.WithContext(ctx)
	for _, tc := range []struct {
		name, filter string
		scoped       bool
	}{
		{"user", "subscriptions.user_id = ?", true},
		{"subscription", "subscriptions.id = ?", true},
		{"whole site", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := db.Model(&model.Subscription{})
			if tc.filter != "" {
				scope = scope.Where(tc.filter, 123)
			}
			query := trafficReconciliationTotalsQuery(db, scope, tc.scoped)
			if query.Statement.Context != ctx {
				t.Fatal("aggregate lost request cancellation")
			}
			result := query.Find(&[]map[string]any{})
			if result.Error != nil {
				t.Fatal(result.Error)
			}
			statement := result.Statement.SQL.String()
			if strings.Contains(statement, "STRAIGHT_JOIN") != tc.scoped {
				t.Fatalf("unexpected join policy: %s", statement)
			}
			if tc.scoped && (!strings.Contains(statement, "FROM (SELECT subscriptions.id FROM `subscriptions` WHERE "+tc.filter) ||
				!strings.Contains(statement, "ON traffic_records.subscription_id = reconciliation_scope.id")) {
				t.Fatalf("scope must drive the traffic join: %s", statement)
			}
			if strings.Contains(statement, "traffic_records.user_id") {
				t.Fatalf("incorrectly hid wrong-owner audit records: %s", statement)
			}
			if tc.scoped && (len(result.Statement.Vars) != 1 || result.Statement.Vars[0] != 123) {
				t.Fatalf("scope argument lost: %v", result.Statement.Vars)
			}
		})
	}
}
