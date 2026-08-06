package datastore

import (
	"os"
	"strings"
	"testing"
)

func TestQuoteMySQLIdentifierEscapesBackticks(t *testing.T) {
	if got := quoteMySQLIdentifier("unsafe`name"); got != "unsafe``name" {
		t.Fatalf("quoteMySQLIdentifier() = %q", got)
	}
}

func TestSubscriptionAccessSchemaInvalidatesLegacyAggregateTokens(t *testing.T) {
	source, err := os.ReadFile("subscription_access_schema.go")
	if err != nil {
		t.Fatalf("read schema source: %v", err)
	}
	text := string(source)
	for _, expected := range []string{
		"DELETE FROM subscription_tokens WHERE subscription_id IS NULL",
		"MODIFY subscription_id bigint unsigned NOT NULL",
		"uq_subscription_token_subscription",
		"idx_subscription_tokens_user",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("subscription access schema missing %q", expected)
		}
	}
	if strings.Contains(text, "ADD UNIQUE KEY uq_subscription_token_user") {
		t.Fatal("subscription access schema restored user-level uniqueness")
	}
}

func TestSubscriptionAccessSchemaPreservesForeignKeyIndexesDuringTransition(t *testing.T) {
	source, err := os.ReadFile("subscription_access_schema.go")
	if err != nil {
		t.Fatalf("read schema source: %v", err)
	}
	text := string(source)

	addUserIndex := strings.Index(text, "ADD KEY idx_subscription_tokens_user (user_id)")
	dropUserUnique := strings.Index(text, "uniqueUserIndexes, err :=")
	if addUserIndex < 0 || dropUserUnique < 0 || addUserIndex > dropUserUnique {
		t.Fatal("user_id replacement index must be installed before dropping the legacy unique index")
	}

	addSubscriptionUnique := strings.Index(text, "ADD UNIQUE KEY uq_subscription_token_subscription (subscription_id)")
	dropSubscriptionIndex := strings.Index(text, "nonUniqueSubscriptionIndexes, err :=")
	if addSubscriptionUnique < 0 || dropSubscriptionIndex < 0 || addSubscriptionUnique > dropSubscriptionIndex {
		t.Fatal("subscription_id unique index must be installed before dropping the foreign-key index")
	}
}
