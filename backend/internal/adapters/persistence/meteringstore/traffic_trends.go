package meteringstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type TrafficTrends struct {
	DB, ReadDB *gorm.DB
	Cache      metering.TrafficTrendCache
}

func (s TrafficTrends) Read(ctx context.Context, actor uint, q metering.TrafficTrendQuery) (metering.TrafficTrendData, error) {
	var out metering.TrafficTrendData
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
		if !q.Administrative {
			q.UserID = actor
		}
		read := tx
		if s.ReadDB != nil && s.ReadDB != s.DB {
			read = s.ReadDB.WithContext(ctx)
		}
		load := func() (metering.TrafficTrendSnapshot, error) {
			result := metering.TrafficTrendSnapshot{AsOf: time.Now().UTC()}
			rows, err := loadTrafficCalendar(read, q)
			if err != nil {
				return result, err
			}
			result.Points, result.RecordCount = metering.BuildTrafficTrendPoints(q.From, q.Days, rows)
			return result, nil
		}
		if s.Cache == nil {
			out.Snapshot, err = load()
		} else {
			cacheQuery := q
			cacheQuery.IncludeSubscriptions = false
			key, _ := json.Marshal(cacheQuery)
			out.Snapshot, err = s.Cache.Get(ctx, sha256.Sum256(key), load)
		}
		if err != nil {
			return err
		}
		out.Subscriptions = make([]metering.TrafficTrendReference, 0)
		if q.UserID > 0 && q.IncludeSubscriptions {
			var rows []struct {
				ID                        uint
				Status, PlanName, SKUName string
			}
			if err := read.Table("subscriptions").Select("subscriptions.id AS id, subscriptions.status AS status, plans.name AS plan_name, plan_skus.name AS sku_name").Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").Where("subscriptions.user_id = ?", q.UserID).Order("subscriptions.created_at DESC, subscriptions.id DESC").Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				name := strings.TrimSpace(row.PlanName)
				if name == "" {
					name = "订阅"
				}
				out.Subscriptions = append(out.Subscriptions, metering.TrafficTrendReference{ID: row.ID, DisplayName: name, Secondary: strings.TrimSpace(row.SKUName), Status: row.Status})
			}
		}
		return nil
	})
	if err != nil {
		return metering.TrafficTrendData{}, err
	}
	return out, nil
}
