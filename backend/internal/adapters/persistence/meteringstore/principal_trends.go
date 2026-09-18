package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
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
		parts := make([]string, 0, len(q.Buckets))
		args := make([]interface{}, 0, 3*len(q.Buckets))
		for _, bucket := range q.Buckets {
			parts = append(parts, "WHEN observed_at >= ? AND observed_at < ? THEN ?")
			args = append(args, bucket.StartUTC, bucket.EndUTC, bucket.Key)
		}
		expression := "CASE " + strings.Join(parts, " ") + " ELSE NULL END"
		read := tx
		if s.ReadDB != nil && s.ReadDB != s.DB {
			read = s.ReadDB.WithContext(ctx)
		}
		return read.Table("principal_flow_scope_observations").Select(expression+" AS day, MAX(active_flows) AS peak, COUNT(*) AS sample_count", args...).Where("scope_type = ? AND scope_id = ? AND observed_at >= ? AND observed_at < ?", scope, id, q.Buckets[0].StartUTC, q.Buckets[len(q.Buckets)-1].EndUTC).Group("day").Order("day ASC").Scan(&rows).Error
	})
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
