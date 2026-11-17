import { authenticatedAPI as api } from './client'
export interface PaymentReceipt {
  id: number; amount_minor: string; currency: string; disposition: string; wallet_entry_id?: number
  created_at: string; ticket_id?: number; resolution_reason?: string
  provider?: string; provider_trade_no?: string
}
export interface PaymentReceiptPage { items: PaymentReceipt[]; has_more: boolean }
export async function fetchPaymentReceipts(order: number, admin: boolean, after: number, signal?: AbortSignal): Promise<PaymentReceiptPage> {
  const { data } = await api.get(`${admin ? '/admin' : ''}/orders/${order}/payment-receipts`, { params: { after, limit: 25 }, signal })
  return data.data
}
export async function resolvePaymentReceipt(order: number, request: { receipt_id: number; ticket_id: number; decision: string; reason: string }): Promise<PaymentReceipt> {
  const { data } = await api.post(`/admin/orders/${order}/payment-receipts/resolve`, request)
  return data.data
}
