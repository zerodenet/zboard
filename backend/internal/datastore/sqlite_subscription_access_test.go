package datastore

import (
	"path/filepath"
	"testing"
)

func TestSQLiteSubscriptionAccessUpgradesLegacyUserIndex(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	for _, statement := range []string{
		"CREATE TABLE subscription_tokens (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, subscription_id INTEGER, token_hash TEXT)",
		"CREATE UNIQUE INDEX idx_subscription_tokens_user_id ON subscription_tokens(user_id)",
		"INSERT INTO subscription_tokens VALUES (1,1,11,'owned'),(2,2,NULL,'legacy')",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := ReconcileSubscriptionAccessSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Table("subscription_tokens").Where("token_hash = 'owned'").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("owned token lost: %d %v", count, err)
	}
	if err := db.Table("subscription_tokens").Where("subscription_id IS NULL").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("aggregate token retained: %d %v", count, err)
	}
	if err := db.Exec("INSERT INTO subscription_tokens VALUES (3,1,12,'second')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO subscription_tokens VALUES (4,1,12,'duplicate')").Error; err == nil {
		t.Fatal("duplicate subscription token accepted")
	}
}
