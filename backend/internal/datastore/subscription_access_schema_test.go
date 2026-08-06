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
