import { authenticatedAPI as api } from './client'
export interface SubscriptionValueQuote { subscription_id: number; root_order_id: number; currency: string; current_minor: string; future_minor: string; total_minor: string; quote_fingerprint: string }
export interface SubscriptionTerminationRequest { quote_fingerprint: string; idempotency_key: string }
export interface SubscriptionTerminationReceipt { id: number; subscription_id: number; currency: string; amount_minor: string; wallet_entry_id?: number; status: string; revocation_status: string; created_at: string }
export async function fetchSubscriptionValue(id: number): Promise<SubscriptionValueQuote> { const { data } = await api.get(`/subscriptions/${id}/value`); return data.data }
export async function terminateSubscription(id: number, request: SubscriptionTerminationRequest): Promise<SubscriptionTerminationReceipt> { const { data } = await api.post(`/subscriptions/${id}/unsubscribe`, request); return data.data }
