import { authenticatedAPI as api } from './client'
export interface WalletEntry {
  id: number; kind: string; balance_delta: string; reserved_delta: string
  balance_after: string; reserved_after: string; source_order_id?: number; reason: string; created_at: string
}
export interface WalletPage {
  currency: string; balance_minor: string; reserved_minor: string; available_minor: string
  entries: WalletEntry[]; has_more: boolean
}
export async function fetchWallet(currency: string, after = 0, signal?: AbortSignal): Promise<WalletPage> {
  const { data } = await api.get('/account/wallet', { params: { currency, after, limit: 50 }, signal })
  return data.data
}
export async function createWalletTopUp(request: { amount_minor: number; currency: string; idempotency_key: string }): Promise<{ id: number }> {
  const { data } = await api.post('/account/wallet/topups', request)
  return data.data
}
