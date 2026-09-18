package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type adminOrderListItem = commerce.OrderListItem
type adminOrderDetail = commerce.OrderDetail
type adminPaymentEventSummary = commerce.PaymentEventSummary

func newAdminOrderListItem(order model.Order) adminOrderListItem {
	return commerce.SummarizeOrder(commerce.Order(order))
}
func newAdminOrderDetail(order model.Order) adminOrderDetail {
	return commerce.DetailOrder(commerce.Order(order))
}
