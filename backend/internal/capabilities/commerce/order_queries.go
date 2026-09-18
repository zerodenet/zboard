package commerce

import (
	"context"
	"strings"
	"time"
)

type OrderQuery struct {
	UserID                    uint
	Search, OrderType, Status string
	From, To                  time.Time
	Offset, Limit             int
	LegacyArray               bool
}
type OrderPage struct {
	Items         []OrderListItem
	Total         int64
	Offset, Limit int
}
type PaymentEventPage struct {
	Items         []PaymentEventSummary
	Total         int64
	Offset, Limit int
}
type OrderQueriesRepository interface {
	List(context.Context, uint, bool, OrderQuery) (OrderPage, error)
	Detail(context.Context, uint, uint) (OrderDetail, error)
	Events(context.Context, uint, uint, int, int) (PaymentEventPage, error)
}
type OrderQueries struct{ Repository OrderQueriesRepository }

func (s OrderQueries) Owned(ctx context.Context, actor uint, q OrderQuery) (OrderPage, error) {
	q.UserID = actor
	q.Search = ""
	q.OrderType = ""
	q.From = time.Time{}
	q.To = time.Time{}
	return s.list(ctx, actor, false, q)
}
func (s OrderQueries) Administrative(ctx context.Context, actor uint, q OrderQuery) (OrderPage, error) {
	return s.list(ctx, actor, true, q)
}
func (s OrderQueries) list(ctx context.Context, actor uint, admin bool, q OrderQuery) (OrderPage, error) {
	if actor == 0 {
		return OrderPage{}, ErrOrderPermission
	}
	q.Search = strings.TrimSpace(q.Search)
	q.Status = strings.TrimSpace(q.Status)
	q.OrderType = strings.TrimSpace(q.OrderType)
	if len(q.Search) > 128 {
		return OrderPage{}, &ValidationError{Message: "q must not exceed 128 bytes"}
	}
	if q.OrderType != "" {
		switch q.OrderType {
		case "new", "renewal", "upgrade", "traffic_pack":
		default:
			return OrderPage{}, &ValidationError{Message: "invalid order_type"}
		}
	}
	if q.Status != "" {
		if _, ok := OrderStatuses(q.Status, admin); !ok {
			return OrderPage{}, &ValidationError{Message: "invalid status"}
		}
	}
	if !q.From.IsZero() || !q.To.IsZero() {
		if q.From.IsZero() || q.To.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour {
			return OrderPage{}, &ValidationError{Message: "invalid order date window"}
		}
	}
	if !q.LegacyArray {
		var err error
		q.Offset, q.Limit, err = orderPageBounds(q.Offset, q.Limit)
		if err != nil {
			return OrderPage{}, err
		}
	}
	return s.Repository.List(ctx, actor, admin, q)
}
func OrderStatuses(status string, admin bool) ([]string, bool) {
	if admin && status == "attention" {
		return []string{"pending", "failed"}, true
	}
	switch status {
	case "pending", "paid", "failed", "canceled", "success":
		return []string{status}, true
	}
	return nil, false
}
func orderPageBounds(offset, limit int) (int, int, error) {
	if limit == 0 {
		limit = 50
	}
	if offset < 0 || limit < 1 || limit > 200 {
		return 0, 0, &ValidationError{Message: "invalid pagination"}
	}
	return offset, limit, nil
}
func (s OrderQueries) Detail(ctx context.Context, actor, id uint) (OrderDetail, error) {
	if actor == 0 {
		return OrderDetail{}, ErrOrderPermission
	}
	if id == 0 {
		return OrderDetail{}, ErrNotFound
	}
	return s.Repository.Detail(ctx, actor, id)
}
func (s OrderQueries) Events(ctx context.Context, actor, id uint, offset, limit int) (PaymentEventPage, error) {
	if actor == 0 {
		return PaymentEventPage{}, ErrOrderPermission
	}
	if id == 0 {
		return PaymentEventPage{}, ErrNotFound
	}
	offset, limit, err := orderPageBounds(offset, limit)
	if err != nil {
		return PaymentEventPage{}, err
	}
	return s.Repository.Events(ctx, actor, id, offset, limit)
}
