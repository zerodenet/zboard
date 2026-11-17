import { authenticatedAPI as subscriptionAccessApi, type ApiRequestOptions } from './client'

export interface SubscriptionAccess {
  configured: boolean
  delivery_configured?: boolean
  subscription_id: number
  token?: string
  token_prefix?: string
  subscription_url?: string
  last_used_at?: string | null
  revoked_at?: string | null
  created_at?: string | null
  updated_at?: string | null
  revoked?: boolean
  notice?: string
}

// Share identity/reauthentication fences while retaining this bounded timeout.
const accessTimeout = 8000

function unwrap<T>(response: any): T {
  return response.data?.data as T
}

function accessPath(subscriptionId: number) {
  if (!Number.isInteger(subscriptionId) || subscriptionId <= 0) {
    throw new Error('subscriptionId must be a positive integer')
  }
  return `/account/subscriptions/${subscriptionId}/access`
}

export async function fetchSubscriptionAccess(subscriptionId: number, options: ApiRequestOptions = {}): Promise<SubscriptionAccess> {
  return unwrap<SubscriptionAccess>(await subscriptionAccessApi.get(accessPath(subscriptionId), { timeout: accessTimeout, signal: options.signal }))
}

export async function rotateSubscriptionAccess(subscriptionId: number): Promise<SubscriptionAccess> {
  return unwrap<SubscriptionAccess>(await subscriptionAccessApi.post(`${accessPath(subscriptionId)}/rotate`, undefined, { timeout: accessTimeout }))
}

export async function revokeSubscriptionAccess(subscriptionId: number): Promise<SubscriptionAccess> {
  return unwrap<SubscriptionAccess>(await subscriptionAccessApi.delete(accessPath(subscriptionId), { timeout: accessTimeout }))
}

// Explicit operator action; keep credentials out of list/detail API responses.
export async function fetchOrderSubscriptionAccess(orderId: number, options: ApiRequestOptions = {}): Promise<SubscriptionAccess> {
  if (!Number.isInteger(orderId) || orderId <= 0) throw new Error('orderId must be a positive integer')
  return unwrap<SubscriptionAccess>(await subscriptionAccessApi.post(`/admin/orders/${orderId}/subscription-access`, undefined, { timeout: accessTimeout, signal: options.signal }))
}
