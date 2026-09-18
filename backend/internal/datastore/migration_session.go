package datastore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// migrationSession pins all queries and session settings to one connection.
// Cleanup uses an independent deadline, including after cancellation or panic.
func migrationSession(ctx context.Context, db *gorm.DB, fn func(*gorm.DB, *sql.Conn) error) error {
	acquire, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return db.WithContext(acquire).Connection(func(session *gorm.DB) error {
		session = session.WithContext(ctx)
		connection, ok := session.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return errors.New("migration requires a dedicated SQL connection")
		}
		return fn(session, connection)
	})
}

func restoreMigrationSession(connection *sql.Conn, statement string, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := connection.ExecContext(ctx, statement, args...); err != nil {
		// Returning a locked or constraint-disabled connection to the pool is unsafe.
		_ = connection.Raw(func(any) error { return driver.ErrBadConn })
		return fmt.Errorf("restore migration session: %w", err)
	}
	return nil
}

// WithMigrationSource supplies the same connection that owns the MySQL read
// lock, so a one-connection pool can be copied without waiting on itself.
func WithMigrationSource(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	if IsSQLite(db) {
		tx := db.WithContext(ctx).Begin()
		if tx.Error != nil {
			return fmt.Errorf("acquire SQLite migration snapshot: %w", tx.Error)
		}
		defer tx.Rollback()
		return fn(tx)
	}
	return migrationSession(ctx, db, func(session *gorm.DB, connection *sql.Conn) (err error) {
		defer func() { err = errors.Join(err, restoreMigrationSession(connection, "UNLOCK TABLES")) }()
		acquire, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if _, err := connection.ExecContext(acquire, "FLUSH TABLES WITH READ LOCK"); err != nil {
			return fmt.Errorf("acquire MySQL migration read lock (grant RELOAD or stop external writers): %w", err)
		}
		return fn(session)
	})
}

// WithCopyDestination owns a single destination transaction and restores the
// previous constraint setting after success, failure, cancellation or panic.
// The caller must have checked the target is empty before preparing its schema.
func WithCopyDestination(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	return migrationSession(ctx, db, func(session *gorm.DB, connection *sql.Conn) (err error) {
		read, disable, restore := "SELECT @@SESSION.FOREIGN_KEY_CHECKS", "SET FOREIGN_KEY_CHECKS = 0", "SET FOREIGN_KEY_CHECKS = ?"
		if IsSQLite(db) {
			read, disable = "PRAGMA foreign_keys", "PRAGMA foreign_keys = OFF"
		}
		var previous int
		if err := connection.QueryRowContext(ctx, read).Scan(&previous); err != nil {
			return err
		}
		if IsSQLite(db) {
			restore = fmt.Sprintf("PRAGMA foreign_keys = %d", previous)
		}
		defer func() {
			var cleanup error
			if IsSQLite(db) {
				cleanup = restoreMigrationSession(connection, restore)
			} else {
				cleanup = restoreMigrationSession(connection, restore, previous)
			}
			err = errors.Join(err, cleanup)
		}()
		if _, err := connection.ExecContext(ctx, disable); err != nil {
			return err
		}
		return session.Transaction(fn)
	})
}
