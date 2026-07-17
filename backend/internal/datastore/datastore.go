package datastore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Open(dataSource string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dataSource), &gorm.Config{
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
	sqlDB.SetMaxOpenConns(64)
	sqlDB.SetMaxIdleConns(16)
	sqlDB.SetConnMaxLifetime(4 * time.Hour)
	return db, nil
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
