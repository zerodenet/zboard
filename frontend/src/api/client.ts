import axios from 'axios'

export const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1'

const tokenKey = 'zboard.auth.token'

const api = axios.create({
  baseURL: API_BASE,
  timeout: 8000
})

api.interceptors.request.use((cfg) => {
  const token = localStorage.getItem(tokenKey)
  if (token) {
    cfg.headers = cfg.headers || {}
    cfg.headers.Authorization = `Bearer ${token}`
  }
  return cfg
})

export function getAuthToken() {
  return localStorage.getItem(tokenKey) || ''
}

export function setAuthToken(token: string) {
  localStorage.setItem(tokenKey, token)
}

export function clearAuthToken() {
  localStorage.removeItem(tokenKey)
}

function unwrap(response: any) {
  return response.data?.data || null
}

export async function getVersion() {
  const response = await api.get('/version')
  return unwrap(response)
}

export async function me() {
  const response = await api.get('/auth/me')
  return unwrap(response)
}

export async function login(account: string, password: string) {
  const response = await api.post('/auth/login', { account, password })
  return unwrap(response)
}

export async function register(body: { username: string; email: string; password: string }) {
  const response = await api.post('/auth/register', body)
  return unwrap(response)
}

export async function fetchNodes() {
  const response = await api.get('/nodes')
  return unwrap(response) || []
}

export async function createNode(payload: any) {
  const response = await api.post('/nodes', payload)
  return unwrap(response)
}

export async function testNodeSSH(nodeId: number) {
  const response = await api.post('/nodes/ssh/test', { node_id: nodeId })
  return unwrap(response)
}

export async function updateNodeSSH(nodeId: number, payload: {
  ssh_host: string
  ssh_port: number
  ssh_user: string
  ssh_password?: string
  ssh_host_key_fingerprint: string
}) {
  const response = await api.put(`/nodes/${nodeId}/ssh`, payload)
  return unwrap(response)
}

export async function rotateNodeReportCredential(nodeId: number) {
  const response = await api.post(`/nodes/${nodeId}/report-credential`)
  return unwrap(response)
}

export async function revokeNodeReportCredential(nodeId: number) {
  const response = await api.delete(`/nodes/${nodeId}/report-credential`)
  return unwrap(response)
}

export async function publishNodeProtocolConfig(nodeId: number, protocol: string, config: string, clientConfig: string) {
  const response = await api.post('/nodes/protocol/config', {
    node_id: nodeId,
    protocol,
    config,
    client_config: clientConfig
  })
  return unwrap(response)
}

export async function fetchPlans() {
  const response = await api.get('/plans')
  return unwrap(response) || []
}

export async function fetchPlansWithOptions(params: { includeInactive?: boolean } = {}) {
  const query = new URLSearchParams()
  if (params.includeInactive) {
    query.set('include_inactive', 'true')
  }
  const path = query.toString() ? `/plans?${query}` : '/plans'
  const response = await api.get(path)
  return unwrap(response) || []
}

export async function createPlan(payload: any) {
  const response = await api.post('/plans', payload)
  return unwrap(response)
}

export async function updatePlan(id: number, payload: any) {
  const response = await api.put(`/admin/plans/${id}`, payload)
  return unwrap(response)
}

export async function fetchOrders(params: { status?: string; userId?: number } = {}) {
  const query = new URLSearchParams()
  if (params.status) {
    query.set('status', params.status)
  }
  if (params.userId) {
    query.set('user_id', String(params.userId))
  }
  const path = query.toString() ? `/orders?${query}` : '/orders'
  const response = await api.get(path)
  return unwrap(response) || []
}

export async function createOrder(planId: number, channel = 'manual') {
  const response = await api.post('/orders', { plan_id: planId, channel })
  return unwrap(response)
}

export async function payOrder(orderId: number) {
  const response = await api.post(`/orders/${orderId}/pay`)
  return unwrap(response)
}

export async function cancelOrder(orderId: number) {
  const response = await api.post(`/orders/${orderId}/cancel`)
  return unwrap(response)
}

export async function fetchSubscriptions() {
  const response = await api.get('/subscriptions')
  return unwrap(response) || []
}

export async function fetchSubscriptionAccess() {
  const response = await api.get('/subscription/access')
  return unwrap(response) || { configured: false }
}

export async function rotateSubscriptionAccess() {
  const response = await api.post('/subscription/access/rotate')
  return unwrap(response)
}

export async function revokeSubscriptionAccess() {
  const response = await api.delete('/subscription/access')
  return unwrap(response)
}

export async function fetchTrafficSummary() {
  const response = await api.get('/traffic/summary')
  return unwrap(response) || {}
}

export async function fetchTrafficRecords(params: { userId?: number; nodeId?: number; subscriptionId?: number } = {}) {
  const query = new URLSearchParams()
  if (params.userId) {
    query.set('user_id', String(params.userId))
  }
  if (params.nodeId) {
    query.set('node_id', String(params.nodeId))
  }
  if (params.subscriptionId) {
    query.set('subscription_id', String(params.subscriptionId))
  }
  const path = query.toString() ? `/traffic/records?${query}` : '/traffic/records'
  const response = await api.get(path)
  return unwrap(response) || []
}

export async function fetchTrafficReconciliation(params: { userId?: number; subscriptionId?: number } = {}) {
  const query = new URLSearchParams()
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.subscriptionId) query.set('subscription_id', String(params.subscriptionId))
  const path = query.toString() ? `/traffic/reconciliation?${query}` : '/traffic/reconciliation'
  const response = await api.get(path)
  return unwrap(response) || []
}

export async function fetchDashboard() {
  const response = await api.get('/admin/dashboard')
  return unwrap(response) || {}
}

export async function fetchAuditLogs(params: { actor?: string; action?: string; target?: string; offset?: number; limit?: number } = {}) {
  const query = new URLSearchParams()
  if (params.actor) query.set('actor', params.actor)
  if (params.action) query.set('action', params.action)
  if (params.target) query.set('target', params.target)
  if (params.offset !== undefined) query.set('offset', String(params.offset))
  if (params.limit !== undefined) query.set('limit', String(params.limit))
  const path = query.toString() ? `/admin/audit-logs?${query}` : '/admin/audit-logs'
  const response = await api.get(path)
  return unwrap(response) || { items: [], total: 0, offset: 0, limit: 50 }
}

export async function fetchUsers(params: { q?: string; status?: string; isAdmin?: boolean } = {}) {
  const query = new URLSearchParams()
  if (params.q) {
    query.set('q', params.q)
  }
  if (params.status) {
    query.set('status', params.status)
  }
  if (params.isAdmin !== undefined) {
    query.set('is_admin', String(params.isAdmin))
  }
  const path = query.toString() ? `/admin/users?${query}` : '/admin/users'
  const response = await api.get(path)
  return unwrap(response) || []
}

export async function createAdminUser(payload: { username: string; email: string; password: string; is_admin?: boolean; status?: string }) {
  const response = await api.post('/admin/users', payload)
  return unwrap(response)
}

export async function updateAdminUser(id: number, payload: { status?: string; is_admin?: boolean; password?: string }) {
  const response = await api.put(`/admin/users/${id}`, payload)
  return unwrap(response)
}

export async function markOrderPaid(orderId: number) {
  const response = await api.post(`/orders/${orderId}/pay?force=true`)
  return unwrap(response)
}
