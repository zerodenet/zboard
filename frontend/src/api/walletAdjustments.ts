import { authenticatedAPI as api } from './client'
import type { WalletPage } from './wallet'
export interface WalletAdjustment {
  id: number; user_id: number; currency: string; amount_minor: string; reason: string
  ticket_id: number; actor_id: number; entry_id: number; reversal_entry_id?: number
  reversal_actor_id?: number; reversal_reason?: string; external_status: string; created_at: string
  results?: { id: number; actor_id: number; outcome: string; reference: string; note: string; occurred_at: string }[]
}
export interface WalletAdjustmentPage { items: WalletAdjustment[]; has_more: boolean }
export interface WalletDebit { user_id: number; currency: string; amount_minor: number; ticket_id: number; reason: string; idempotency_key: string }
export interface WalletPayout { outcome: string; reference: string; note: string; occurred_at: string; idempotency_key: string }
export async function fetchAdministrativeWallet(user: number, currency: string, after = 0, signal?: AbortSignal): Promise<WalletPage> {
  const { data } = await api.get(`/admin/users/${user}/wallet`, { params: { currency, after, limit: 25 }, signal }); return data.data
}
export async function fetchWalletAdjustments(user: number, currency: string, after = 0, signal?: AbortSignal): Promise<WalletAdjustmentPage> {
  const { data } = await api.get('/admin/wallet-adjustments', { params: { user_id: user, currency, after, limit: 25 }, signal }); return data.data
}
export async function fetchWalletAdjustment(id: number): Promise<WalletAdjustment> { const { data } = await api.get(`/admin/wallet-adjustments/${id}`); return data.data }
export async function debitWallet(request: WalletDebit): Promise<WalletAdjustment> { const { data } = await api.post('/admin/wallet-adjustments', request); return data.data }
export async function recordWalletPayout(id: number, request: WalletPayout): Promise<WalletAdjustment> { const { data } = await api.post(`/admin/wallet-adjustments/${id}/payout-results`, request); return data.data }
export async function reverseWalletAdjustment(id: number, reason: string): Promise<WalletAdjustment> { const { data } = await api.post(`/admin/wallet-adjustments/${id}/reverse`, { reason }); return data.data }
