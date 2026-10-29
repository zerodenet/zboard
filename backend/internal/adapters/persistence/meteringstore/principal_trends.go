package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PrincipalTrends struct{ DB, ReadDB *gorm.DB }

func (s PrincipalTrends) Read(ctx context.Context, actor uint, q metering.PrincipalTrendQuery) ([]metering.PrincipalTrendRow, error) {
	rows := make([]metering.PrincipalTrendRow, 0)
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "is_admin", "status").First(&user, actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return metering.ErrTrendPermission
		}
		if err != nil {
			return err
		}
		if user.Status != "active" || (q.Administrative && !user.IsAdmin) {
			return metering.ErrTrendPermission
		}
		scope, id := ScopeUser, q.UserID
		if !q.Administrative {
			id = actor
		}
		if q.SubscriptionID > 0 {
			var sub model.Subscription
			lookup := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id = ?", q.SubscriptionID)
			if !q.Administrative {
				lookup = lookup.Where("user_id = ?", actor)
			}
			err = lookup.First(&sub).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			scope, id = ScopeSubscription, q.SubscriptionID
		}
		if id == 0 {
			return nil
		}
		read := tx
		if s.ReadDB != nil && s.ReadDB != s.DB {
			return s.ReadDB.WithContext(ctx).Transaction(func(snapshot *gorm.DB) error {
				var err error
				rows, err = readPrincipalStatistics(snapshot, scope, id, q.Buckets)
				return err
			}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		}
		rows, err = readPrincipalStatistics(read, scope, id, q.Buckets)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if len(rows[i].Day) >= 10 {
			rows[i].Day = rows[i].Day[:10]
		}
	}
	return rows, nil
}
