import axios from 'axios'
import { API_BASE, getAuthToken, type ApiRequestOptions } from './client'
import { normalizeApiErrorPayload } from '../utils/apiError'

export interface SubscriptionAccess {
  configured: boolean
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

const subscriptionAccessApi = axios.create({
  baseURL: API_BASE,
  timeout: 8000,
})

subscriptionAccessApi.interceptors.request.use(config => {
  const token = getAuthToken()
  if (token) {
    config.headers = config.headers || {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

subscriptionAccessApi.interceptors.response.use(
  response => response,
  cause => {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    return Promise.reject(cause)
  },
)

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
  return unwrap<SubscriptionAccess>(await subscriptionAccessApi.get(accessPath(subscriptionId), { signal: options.signal }))
}

export async function rotateSubscriptionAccess(subscriptionId: number): Promise<SubscriptionAccess> {
  return unwrap<SubscriptionAccess>(await subscriptionAccessApi.post(`${accessPath(subscriptionId)}/rotate`))
}

export async function revokeSubscriptionAccess(subscriptionId: number): Promise<SubscriptionAccess> {
  return unwrap<SubscriptionAccess>(await subscriptionAccessApi.delete(accessPath(subscriptionId)))
}
