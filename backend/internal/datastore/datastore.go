package datastore

import (
	"fmt"
	"time"

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

func MustDSN(raw string) string {
	if raw == "" {
		return "root:root@tcp(127.0.0.1:3306)/zboard?charset=utf8mb4&parseTime=true&loc=Local"
	}
	return raw
}

func Ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func QuoteDSN(dataSource string) string {
	return fmt.Sprintf("DSN=%s", dataSource)
}
