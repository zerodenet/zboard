package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

const createSchemaMigrationsSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(191) NOT NULL PRIMARY KEY,
  applied_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// RunMigrations applies every embedded .up.sql migration exactly once. MySQL
// DDL commits implicitly, so a migration is recorded only after all of its
// statements succeed; failures stop startup and keep the failing version
// unapplied for operator inspection.
func RunMigrations(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	if _, err := sqlDB.Exec(createSchemaMigrationsSQL); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	if err := adoptLegacyAutoMigrateSchema(sqlDB, versions); err != nil {
		return err
	}

	for _, version := range versions {
		var applied int
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		payload, err := migrations.Files.ReadFile(version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		statements, err := splitMigrationStatements(string(payload))
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", version, err)
		}
		for index, statement := range statements {
			if _, err := sqlDB.Exec(statement); err != nil {
				return fmt.Errorf("apply migration %s statement %d: %w", version, index+1, err)
			}
		}
		if _, err := sqlDB.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", version, time.Now().UTC()); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}
	return nil
}

func adoptLegacyAutoMigrateSchema(db *sql.DB, versions []string) error {
	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
		return fmt.Errorf("inspect schema migration history: %w", err)
	}
	if applied > 0 {
		return nil
	}

	tableExists := func(name string) (bool, error) {
		var count int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			name,
		).Scan(&count)
		return count > 0, err
	}
	columnExists := func(table, column string) (bool, error) {
		var count int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
			table, column,
		).Scan(&count)
		return count > 0, err
	}

	usersExist, err := tableExists("users")
	if err != nil {
		return fmt.Errorf("inspect legacy schema: %w", err)
	}
	if !usersExist {
		return nil
	}
	for _, table := range []string{"installations", "plan_skus", "protocol_endpoints", "plan_protocol_endpoints"} {
		exists, err := tableExists(table)
		if err != nil {
			return fmt.Errorf("inspect legacy table %s: %w", table, err)
		}
		if !exists {
			return fmt.Errorf("unversioned legacy database is older than the supported AutoMigrate baseline; missing table %s", table)
		}
	}
	runtimeKeyExists, err := columnExists("protocol_endpoints", "runtime_key")
	if err != nil {
		return fmt.Errorf("inspect legacy protocol schema: %w", err)
	}
	if !runtimeKeyExists {
		return errors.New("unversioned legacy database is missing protocol_endpoints.runtime_key")
	}

	for _, version := range versions {
		if version > "0009_commerce_protocol_endpoints.up.sql" {
			break
		}
		if _, err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", version, time.Now().UTC()); err != nil {
			return fmt.Errorf("adopt legacy migration %s: %w", version, err)
		}
	}
	return nil
}

func splitMigrationStatements(source string) ([]string, error) {
	statements := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false
	lineComment := false
	blockComment := false
	runes := []rune(source)

	flush := func() {
		statement := strings.TrimSpace(current.String())
		if statement != "" {
			statements = append(statements, statement)
		}
		current.Reset()
	}

	for index := 0; index < len(runes); index++ {
		char := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}

		if lineComment {
			if char == '\n' {
				lineComment = false
				current.WriteRune(char)
			}
			continue
		}
		if blockComment {
			if char == '*' && next == '/' {
				blockComment = false
				index++
			}
			continue
		}
		if quote == 0 {
			if char == '-' && next == '-' {
				lineComment = true
				index++
				continue
			}
			if char == '#' {
				lineComment = true
				continue
			}
			if char == '/' && next == '*' {
				blockComment = true
				index++
				continue
			}
			if char == '\'' || char == '"' || char == '`' {
				quote = char
				current.WriteRune(char)
				continue
			}
			if char == ';' {
				flush()
				continue
			}
			current.WriteRune(char)
			continue
		}

		current.WriteRune(char)
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quote != '`' {
			escaped = true
			continue
		}
		if char == quote {
			if next == quote && quote != '`' {
				current.WriteRune(next)
				index++
				continue
			}
			quote = 0
		}
	}

	if quote != 0 || blockComment {
		return nil, fmt.Errorf("unterminated quoted value or block comment")
	}
	flush()
	return statements, nil
}
