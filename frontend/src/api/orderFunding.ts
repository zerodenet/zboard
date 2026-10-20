import { authenticatedAPI as api } from './client'
export interface OrderFunding {
  change_mode?: 'full_term'; settlement_minor?: string; surplus_minor?: string
  entitlement_minor?: string; reserved_minor?: string
  order_id: number; configured: boolean; use_balance: boolean; currency: string
  amount_minor: string; available_minor: string; wallet_minor: string; cash_minor: string
  state: string; expires_at?: string; expired: boolean; release_reason?: string
}
export async function fetchOrderFunding(order: number, signal?: AbortSignal): Promise<OrderFunding> {
  const { data } = await api.get(`/orders/${order}/funding`, { signal }); return data.data
}
export async function configureOrderFunding(order: number, useBalance: boolean): Promise<OrderFunding> {
  const { data } = await api.post(`/orders/${order}/funding`, { use_balance: useBalance }); return data.data
}
export async function payOrderFromWallet(order: number): Promise<{ order_id: number; status: string; fulfilled: boolean }> {
  const { data } = await api.post(`/orders/${order}/wallet-pay`, {}); return data.data
}
