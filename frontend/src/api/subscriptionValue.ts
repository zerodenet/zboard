import { authenticatedAPI as api } from './client'
export interface SubscriptionValueQuote { subscription_id: number; root_order_id: number; currency: string; current_minor: string; future_minor: string; total_minor: string; quote_fingerprint: string }
export interface SubscriptionTerminationRequest { quote_fingerprint: string; idempotency_key: string }
export interface SubscriptionTerminationReceipt { record_kind?: string; id: number; subscription_id: number; currency: string; amount_minor: string; wallet_entry_id?: number; status: string; revocation_status: string; created_at: string }
export async function fetchSubscriptionValue(id: number): Promise<SubscriptionValueQuote> { const { data } = await api.get(`/subscriptions/${id}/value`); return data.data }
export async function terminateSubscription(id: number, request: SubscriptionTerminationRequest): Promise<SubscriptionTerminationReceipt> { const { data } = await api.post(`/subscriptions/${id}/unsubscribe`, request); return data.data }

export interface AdministrativeSubscriptionValueQuote extends Omit<SubscriptionValueQuote, 'root_order_id' | 'current_minor' | 'future_minor' | 'total_minor'> {
 user_id: number; root_order_id?: number; current_minor: string | null; future_minor: string | null; total_minor: string | null; max_refund_minor: string; valuation_status?: 'verified' | 'unavailable'; message?: string
}
export interface AdministrativeSubscriptionTerminationRequest extends SubscriptionTerminationRequest { amount_minor: number; reason: string }
export async function fetchAdministrativeSubscriptionValue(id: number): Promise<AdministrativeSubscriptionValueQuote> { const { data } = await api.get(`/admin/subscriptions/${id}/value`); return data.data }
export async function administrativelyTerminateSubscription(id: number, request: AdministrativeSubscriptionTerminationRequest): Promise<SubscriptionTerminationReceipt> { const { data } = await api.post(`/admin/subscriptions/${id}/unsubscribe`, request); return data.data }
