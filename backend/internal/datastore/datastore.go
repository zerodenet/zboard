package datastore

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/libtnb/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	DriverMySQL  = "mysql"
	DriverSQLite = "sqlite"
)

type PoolConfig struct {
	MaxOpenConnections int
	MaxIdleConnections int
	ConnectionLifetime time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpenConnections: 8,
		MaxIdleConnections: 2,
		ConnectionLifetime: time.Hour,
	}
}

func Open(dataSource string, options ...PoolConfig) (*gorm.DB, error) {
	return OpenWithDriver(DriverMySQL, dataSource, options...)
}

func OpenWithDriver(driver, dataSource string, options ...PoolConfig) (*gorm.DB, error) {
	driver = strings.ToLower(strings.TrimSpace(driver))
	var dialector gorm.Dialector
	switch driver {
	case DriverMySQL:
		dialector = mysql.Open(dataSource)
	case DriverSQLite:
		dialector = sqlite.Open(normalizeSQLiteDSN(dataSource))
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool := DefaultPoolConfig()
	if len(options) > 0 {
		pool = options[0]
	}
	if driver == DriverSQLite {
		pool.MaxOpenConnections = 1
		pool.MaxIdleConnections = 1
		pool.ConnectionLifetime = 0
	}
	sqlDB.SetMaxOpenConns(pool.MaxOpenConnections)
	sqlDB.SetMaxIdleConns(pool.MaxIdleConnections)
	sqlDB.SetConnMaxLifetime(pool.ConnectionLifetime)
	return db, nil
}

func normalizeSQLiteDSN(dataSource string) string {
	dsn := strings.TrimSpace(dataSource)
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
}

func Ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func QuoteDSN(dataSource string) string {
	parsed, err := mysqlconfig.ParseDSN(dataSource)
	if err != nil {
		return "DSN=<invalid or redacted>"
	}
	if parsed.Passwd != "" {
		parsed.Passwd = "***"
	}
	return fmt.Sprintf("DSN=%s", parsed.FormatDSN())
}

func QuoteDataSource(driver, dataSource string) string {
	if strings.EqualFold(strings.TrimSpace(driver), DriverSQLite) {
		path := strings.SplitN(strings.TrimSpace(dataSource), "?", 2)[0]
		if path == ":memory:" {
			return "SQLite=:memory:"
		}
		return "SQLite=" + filepath.Clean(path)
	}
	return QuoteDSN(dataSource)
}

func ValidateDataSource(driver, dataSource string, production bool) error {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case DriverMySQL, "":
		return ValidateDSN(dataSource, production)
	case DriverSQLite:
		value := strings.TrimSpace(dataSource)
		if value == "" {
			return errors.New("sqlite datasource path is required")
		}
		if production && (value == ":memory:" || strings.Contains(value, "mode=memory")) {
			return errors.New("production sqlite datasource must be a persistent file")
		}
		return nil
	default:
		return fmt.Errorf("unsupported database driver %q", driver)
	}
}

func ValidateDSN(dataSource string, production bool) error {
	parsed, err := mysqlconfig.ParseDSN(strings.TrimSpace(dataSource))
	if err != nil {
		return fmt.Errorf("invalid datasource: %w", err)
	}
	if parsed.User == "" {
		return errors.New("datasource user is required")
	}
	if parsed.Passwd == "" {
		return errors.New("datasource password is required")
	}
	if production {
		if strings.EqualFold(parsed.User, "root") {
			return errors.New("production datasource must not use the root database account")
		}
		normalizedPassword := strings.ToLower(strings.TrimSpace(parsed.Passwd))
		switch normalizedPassword {
		case "password", "root", "admin", "admin123", "changeme", "change-me":
			return errors.New("production datasource uses a known weak password")
		}
		if strings.HasPrefix(normalizedPassword, "generate-") ||
			strings.HasPrefix(normalizedPassword, "replace-") {
			return errors.New("production datasource uses a placeholder password")
		}
	}
	if parsed.DBName == "" {
		return errors.New("datasource database name is required")
	}
	return nil
}
