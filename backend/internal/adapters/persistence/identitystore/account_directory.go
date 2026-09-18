package identitystore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type AccountDirectory struct{ DB *gorm.DB }

type accountBusinessCounts struct {
	Active int64 `gorm:"column:active_count"`
	Total  int64 `gorm:"column:total_count"`
}

func (s AccountDirectory) List(ctx context.Context, q identity.AccountDirectoryQuery, now time.Time) (identity.AccountDirectoryPage, error) {
	db := s.DB.WithContext(ctx)
	query := db.Model(&model.User{})
	if q.Status != "" {
		query = query.Where("status = ?", q.Status)
	}
	if q.IsAdmin != nil {
		query = query.Where("is_admin = ?", *q.IsAdmin)
	}
	if q.Search != "" {
		like := fmt.Sprintf("%%%s%%", strings.ToLower(q.Search))
		query = query.Where("LOWER(email) LIKE ? OR LOWER(account_name) LIKE ?", like, like)
	}
	var total int64
	if q.Paged {
		if err := query.Count(&total).Error; err != nil {
			return identity.AccountDirectoryPage{}, err
		}
		query = query.Offset(q.Offset).Limit(q.Limit)
	}
	sortColumn := map[string]string{"id": "id", "email": "email", "created_at": "created_at"}[q.Sort]
	if sortColumn == "" {
		sortColumn = "created_at"
	}
	direction := "desc"
	if q.Direction == "asc" {
		direction = "asc"
	}
	order := sortColumn + " " + direction
	if sortColumn != "id" {
		order += ", id " + direction
	}
	var users []model.User
	if err := query.Order(order).Find(&users).Error; err != nil {
		return identity.AccountDirectoryPage{}, err
	}
	items, err := s.project(ctx, users, now)
	return identity.AccountDirectoryPage{Items: items, Total: total}, err
}

func (s AccountDirectory) project(ctx context.Context, users []model.User, now time.Time) ([]identity.AccountDirectoryItem, error) {
	items := make([]identity.AccountDirectoryItem, 0, len(users))
	if len(users) == 0 {
		return items, nil
	}
	ids := make([]uint, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	type countRow struct {
		UserID uint  `gorm:"column:user_id"`
		Active int64 `gorm:"column:active_count"`
		Total  int64 `gorm:"column:total_count"`
	}
	var subscriptions []countRow
	if err := s.DB.WithContext(ctx).Model(&model.Subscription{}).
		Select(`user_id, COUNT(*) AS total_count, COALESCE(SUM(CASE WHEN status = ? AND end_at > ? AND flow_used < flow_total THEN 1 ELSE 0 END), 0) AS active_count`, "active", now).
		Where("user_id IN ?", ids).Group("user_id").Scan(&subscriptions).Error; err != nil {
		return nil, err
	}
	var orders []countRow
	if err := s.DB.WithContext(ctx).Model(&model.Order{}).
		Select(`user_id, COUNT(*) AS total_count, COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS active_count`, "pending").
		Where("user_id IN ?", ids).Group("user_id").Scan(&orders).Error; err != nil {
		return nil, err
	}
	subscriptionCounts, orderCounts := map[uint]accountBusinessCounts{}, map[uint]accountBusinessCounts{}
	for _, row := range subscriptions {
		subscriptionCounts[row.UserID] = accountBusinessCounts{Active: row.Active, Total: row.Total}
	}
	for _, row := range orders {
		orderCounts[row.UserID] = accountBusinessCounts{Active: row.Active, Total: row.Total}
	}
	for _, user := range users {
		subs, orders := subscriptionCounts[user.ID], orderCounts[user.ID]
		items = append(items, identity.AccountDirectoryItem{
			PublicAccount: publicAccount(user), CreatedAt: user.CreatedAt,
			ActiveSubscriptionCount: subs.Active, TotalSubscriptionCount: subs.Total,
			PendingOrderCount: orders.Active, TotalOrderCount: orders.Total,
		})
	}
	return items, nil
}

func (s AccountDirectory) Detail(ctx context.Context, id uint, now time.Time) (identity.AccountBusinessDetail, error) {
	var user model.User
	if err := s.DB.WithContext(ctx).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identity.AccountBusinessDetail{}, identity.ErrAccountNotFound
		}
		return identity.AccountBusinessDetail{}, err
	}
	var subs accountBusinessCounts
	err := s.DB.WithContext(ctx).Model(&model.Subscription{}).
		Select("COUNT(*) AS total_count, COALESCE(SUM(CASE WHEN status = ? AND end_at > ? AND flow_used < flow_total THEN 1 ELSE 0 END), 0) AS active_count", "active", now).
		Where("user_id = ?", id).Scan(&subs).Error
	if err != nil {
		return identity.AccountBusinessDetail{}, err
	}
	var orders accountBusinessCounts
	err = s.DB.WithContext(ctx).Model(&model.Order{}).
		Select("COUNT(*) AS total_count, COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS active_count", "pending").
		Where("user_id = ?", id).Scan(&orders).Error
	if err != nil {
		return identity.AccountBusinessDetail{}, err
	}
	return identity.AccountBusinessDetail{
		ID: user.ID, AccountName: user.AccountName, Email: user.Email, EmailVerifiedAt: user.EmailVerifiedAt,
		LastLoginAt: user.LastLoginAt, IsAdmin: user.IsAdmin, Status: user.Status,
		ActiveSubscriptionCount: subs.Active, TotalSubscriptionCount: subs.Total,
		PendingOrderCount: orders.Active, TotalOrderCount: orders.Total,
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}, nil
}
