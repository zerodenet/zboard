import type { AdminOrderListItem } from '../api/client'

export function accountOrderName(order: AdminOrderListItem) {
  if (order.order_type === 'topup') return '余额充值'
  return order.plan_name || `订单 #${order.id}`
}
