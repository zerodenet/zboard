package datastore

import (
	"strings"
	"testing"
)

func TestQuoteDSNRedactsPassword(t *testing.T) {
	quoted := QuoteDSN("zboard:super-secret@tcp(mysql:3306)/zboard?parseTime=true")
	if strings.Contains(quoted, "super-secret") {
		t.Fatalf("QuoteDSN() leaked password: %s", quoted)
	}
	if !strings.Contains(quoted, "***") || !strings.Contains(quoted, "mysql:3306") {
		t.Fatalf("QuoteDSN() = %q, want redaction and endpoint", quoted)
	}

	invalid := QuoteDSN("not a valid dsn with secret")
	if strings.Contains(invalid, "secret") {
		t.Fatalf("QuoteDSN() leaked invalid input: %s", invalid)
	}
}

func TestValidateDSNProductionRules(t *testing.T) {
	if err := ValidateDSN("zboard:strong-db-password@tcp(mysql:3306)/zboard", true); err != nil {
		t.Fatalf("ValidateDSN() error = %v", err)
	}
	for _, dsn := range []string{
		"root:strong-db-password@tcp(mysql:3306)/zboard",
		"zboard:password@tcp(mysql:3306)/zboard",
		"zboard:generate-a-strong-zboard-db-password@tcp(mysql:3306)/zboard",
		"zboard:@tcp(mysql:3306)/zboard",
		"zboard:strong-db-password@tcp(mysql:3306)/",
	} {
		if err := ValidateDSN(dsn, true); err == nil {
			t.Fatalf("ValidateDSN(%q) error = nil, want rejection", dsn)
		}
	}
}
