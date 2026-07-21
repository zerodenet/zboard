package datastore

import (
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/migrations"
)

func TestSplitMigrationStatements(t *testing.T) {
	source := `
-- a leading comment
CREATE TABLE demo (value VARCHAR(32));
INSERT INTO demo (value) VALUES ('a;b'), ("c;d");
/* a block ; comment */
UPDATE demo SET value = 'it''s safe'; # trailing comment
`
	statements, err := splitMigrationStatements(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("got %d statements: %#v", len(statements), statements)
	}
}

func TestSplitMigrationStatementsRejectsUnterminatedInput(t *testing.T) {
	if _, err := splitMigrationStatements("SELECT 'broken;"); err == nil {
		t.Fatal("expected unterminated quote error")
	}
}

func TestEveryEmbeddedUpMigrationParses(t *testing.T) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		payload, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		statements, err := splitMigrationStatements(string(payload))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if len(statements) == 0 {
			t.Fatalf("%s contains no statements", entry.Name())
		}
	}
}
