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

export interface SetupStatus {
	installed: boolean
	site_name?: string
	site_url?: string
	allow_registration?: boolean
	version: string
}

export interface InstallRequest {
	site_name: string
	site_url: string
	allow_registration: boolean
	admin_email: string
	admin_password: string
}

export async function getSetupStatus(): Promise<SetupStatus> {
	const response = await api.get('/setup/status')
	return unwrap(response)
}

export async function installZboard(body: InstallRequest) {
	const response = await api.post('/setup/install', body)
	return unwrap(response)
}

export async function updateSiteSettings(body: Pick<InstallRequest, 'site_name' | 'site_url' | 'allow_registration'>) {
	const response = await api.put('/admin/settings', body)
	return unwrap(response)
}

export interface SystemConfig {
	id: number
	config_key: string
	name: string
	value?: unknown
	value_type: 'string' | 'bool' | 'int' | 'json'
	description: string
	is_public: boolean
	is_secret: boolean
	configured: boolean
	revision: number
	updated_at: string
}

export async function fetchSystemConfigs(): Promise<SystemConfig[]> {
	const response = await api.get('/admin/system-configs')
	return unwrap(response) || []
}

export async function fetchPublicSystemConfigs(): Promise<SystemConfig[]> {
	const response = await api.get('/system/configs')
	return unwrap(response) || []
}

export async function updateSystemConfig(key: string, value: unknown, expectedRevision: number): Promise<SystemConfig> {
	const response = await api.put(`/admin/system-configs/${encodeURIComponent(key)}`, {
		value,
		expected_revision: expectedRevision
	})
	return unwrap(response)
}

export async function getVersion() {
  const response = await api.get('/version')
  return unwrap(response)
}

export async function me() {
  const response = await api.get('/auth/me')
  return unwrap(response)
}

export async function login(email: string, password: string) {
	const response = await api.post('/auth/login', { email, password })
  return unwrap(response)
}

export async function register(body: { email: string; password: string }) {
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

export async function updateNode(nodeId: number, payload: any) {
  const response = await api.put(`/nodes/${nodeId}`, payload)
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
  ssh_auth_method: 'password' | 'private_key'
  ssh_privilege_mode: 'none' | 'sudo' | 'su'
  ssh_password?: string
  ssh_private_key?: string
  ssh_private_key_passphrase?: string
  ssh_privilege_password?: string
}) {
  const response = await api.put(`/nodes/${nodeId}/ssh`, payload)
  return unwrap(response)
}

export async function resetNodeSSHHostKey(nodeId: number) {
  const response = await api.post(`/nodes/${nodeId}/ssh/host-key/reset`)
  return unwrap(response)
}

export interface NodeKernelState {
  node_id: number
  status: 'unknown' | 'not_installed' | 'healthy' | 'degraded' | 'failed' | 'unsupported'
  phase: string
  recommended_action: string
  platform_os: string
  architecture: string
  libc: string
  desired_version: string
  installed_version: string
  desired_sha256: string
  installed_sha256: string
  desired_config_sha256: string
  applied_config_sha256: string
  service_status: string
  control_status: string
  last_error: string
  active_operation_id?: number
  last_detected_at?: string
  last_healthy_at?: string
}

export interface NodeKernelOperation {
  id: number
  node_id: number
  operation_type: string
  status: 'running' | 'succeeded' | 'failed'
  phase: string
  desired_version: string
  result_summary: string
  error: string
  created_at: string
}

export interface ZeroRelease {
  version: string
  tag: string
  artifact_url: string
  artifact_sha256: string
  artifact_size: number
}

export async function fetchNodeKernel(nodeId: number): Promise<{ state: NodeKernelState; operations: NodeKernelOperation[] }> {
  const response = await api.get(`/nodes/${nodeId}/kernel`)
  return unwrap(response)
}

export async function detectNodeKernel(nodeId: number) {
  const response = await api.post(`/nodes/${nodeId}/kernel/detect`)
  return unwrap(response)
}

export async function reconcileNodeKernel(nodeId: number) {
  const response = await api.post(`/nodes/${nodeId}/kernel/reconcile`, undefined, { timeout: 300_000 })
  return unwrap(response)
}

export async function fetchLatestZeroRelease(): Promise<ZeroRelease> {
  const response = await api.get('/admin/kernel/releases/latest')
  return unwrap(response)
}

export interface NodeSSHTerminalTicket {
	ticket: string
	expires_at: number
}

export async function createNodeSSHTerminalTicket(nodeId: number): Promise<NodeSSHTerminalTicket> {
	const response = await api.post(`/nodes/${nodeId}/ssh/terminal-ticket`)
	return unwrap(response)
}

export function buildNodeSSHTerminalURL(nodeId: number, ticket: string): string {
	const base = new URL(API_BASE, `${window.location.origin}/`)
	base.protocol = base.protocol === 'https:' ? 'wss:' : 'ws:'
	base.pathname = `${base.pathname.replace(/\/$/, '')}/nodes/${nodeId}/ssh/terminal`
	base.search = ''
	base.searchParams.set('ticket', ticket)
	return base.toString()
}

export async function rotateNodeConnectorCredential(nodeId: number) {
  const response = await api.post(`/nodes/${nodeId}/connector-credential`)
  return unwrap(response)
}

export async function revokeNodeConnectorCredential(nodeId: number) {
  const response = await api.delete(`/nodes/${nodeId}/connector-credential`)
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

export async function createProtocolEndpoint(payload: any) {
	const response = await api.post('/admin/protocol-endpoints', payload)
  return unwrap(response)
}

export async function fetchProtocolEndpoints() {
	const response = await api.get('/admin/protocol-endpoints')
	return unwrap(response) || []
}

export async function updateProtocolEndpoint(id: number, payload: any) {
	const response = await api.put(`/admin/protocol-endpoints/${id}`, payload)
	return unwrap(response)
}

export async function deployProtocolEndpoint(id: number) {
	const response = await api.post(`/admin/protocol-endpoints/${id}/deploy`)
	return unwrap(response)
}

export type ProtocolDeployment = {
  id: number
  protocol_endpoint_id: number
  node_id: number
  config_revision: number
  status: 'running' | 'succeeded' | 'failed'
  output?: string
  error?: string
  started_at?: string
  finished_at?: string
  created_at: string
}

export async function fetchProtocolEndpoint(id: number) {
  const response = await api.get(`/admin/protocol-endpoints/${id}`)
  return unwrap(response)
}

export async function fetchProtocolDeployments(params: { nodeId?: number; protocolEndpointId?: number; status?: string; offset?: number; limit?: number } = {}) {
  const query = new URLSearchParams()
  if (params.nodeId) query.set('node_id', String(params.nodeId))
  if (params.protocolEndpointId) query.set('protocol_endpoint_id', String(params.protocolEndpointId))
  if (params.status) query.set('status', params.status)
  if (params.offset !== undefined) query.set('offset', String(params.offset))
  if (params.limit !== undefined) query.set('limit', String(params.limit))
  const path = query.toString() ? `/admin/protocol-deployments?${query}` : '/admin/protocol-deployments'
  const response = await api.get(path)
  return unwrap(response) || { items: [], total: 0, offset: 0, limit: 50 }
}

export async function fetchNodeGroups() {
	const response = await api.get('/admin/node-groups')
	return unwrap(response) || []
}

export async function createNodeGroup(payload: any) {
	const response = await api.post('/admin/node-groups', payload)
	return unwrap(response)
}

export async function updateNodeGroup(id: number, payload: any) {
	const response = await api.put(`/admin/node-groups/${id}`, payload)
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

export async function createPlanSKU(planId: number, payload: any) {
	const response = await api.post(`/admin/plans/${planId}/skus`, payload)
	return unwrap(response)
}

export async function updatePlanSKU(id: number, payload: any) {
	const response = await api.put(`/admin/plan-skus/${id}`, payload)
	return unwrap(response)
}

export async function fetchOrders(params: { status?: string; userId?: number } = {}, admin = false) {
  const query = new URLSearchParams()
  if (params.status) {
    query.set('status', params.status)
  }
  if (params.userId) {
    query.set('user_id', String(params.userId))
  }
  const base = admin ? '/admin/orders' : '/orders'
  const path = query.toString() ? `${base}?${query}` : base
  const response = await api.get(path)
  return unwrap(response) || []
}

export async function createOrder(planSkuId: number, options: { channel?: string; orderType?: string; targetSubscriptionId?: number } = {}) {
	const response = await api.post('/orders', {
		plan_sku_id: planSkuId,
		channel: options.channel || 'manual',
		order_type: options.orderType || 'new',
		target_subscription_id: options.targetSubscriptionId || undefined
	})
  return unwrap(response)
}

export async function cancelOrder(orderId: number, admin = false) {
  const response = await api.post(`${admin ? '/admin' : ''}/orders/${orderId}/cancel`)
  return unwrap(response)
}

export async function fetchSubscriptions(params: { userId?: number; status?: string } = {}, admin = false) {
  const query = new URLSearchParams()
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.status) query.set('status', params.status)
  const base = admin ? '/admin/subscriptions' : '/subscriptions'
  const path = query.toString() ? `${base}?${query}` : base
  const response = await api.get(path)
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

export async function fetchTrafficSummary(admin = false) {
  const response = await api.get(`${admin ? '/admin' : ''}/traffic/summary`)
  return unwrap(response) || {}
}

export async function fetchTrafficRecords(params: { userId?: number; nodeId?: number; protocolEndpointId?: number; subscriptionId?: number } = {}, admin = false) {
  const query = new URLSearchParams()
  if (params.userId) {
    query.set('user_id', String(params.userId))
  }
  if (params.nodeId) {
    query.set('node_id', String(params.nodeId))
  }
	if (params.protocolEndpointId) query.set('protocol_endpoint_id', String(params.protocolEndpointId))
  if (params.subscriptionId) {
    query.set('subscription_id', String(params.subscriptionId))
  }
  const base = admin ? '/admin/traffic/records' : '/traffic/records'
  const path = query.toString() ? `${base}?${query}` : base
  const response = await api.get(path)
  return unwrap(response) || []
}

export async function fetchTrafficReconciliation(params: { userId?: number; subscriptionId?: number } = {}, admin = false) {
  const query = new URLSearchParams()
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.subscriptionId) query.set('subscription_id', String(params.subscriptionId))
  const base = admin ? '/admin/traffic/reconciliation' : '/traffic/reconciliation'
  const path = query.toString() ? `${base}?${query}` : base
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

export async function createAdminUser(payload: { email: string; password: string; is_admin?: boolean; status?: string }) {
  const response = await api.post('/admin/users', payload)
  return unwrap(response)
}

export async function updateAdminUser(id: number, payload: { status?: string; is_admin?: boolean; password?: string }) {
  const response = await api.put(`/admin/users/${id}`, payload)
  return unwrap(response)
}

export async function markOrderPaid(orderId: number) {
  const response = await api.post(`/admin/orders/${orderId}/pay?force=true`)
  return unwrap(response)
}

export interface AdminTask {
	id: number
	type: 'quota' | 'email'
	scope: string
	content: string
	status: number
	errors: string
	total: number
	current: number
	idempotency_key: string
	priority: number
	attempts: number
	max_attempts: number
	started_at?: string
	finished_at?: string
	created_at: string
	updated_at: string
}

export interface AdminTaskCreateRequest {
	type: 'quota' | 'email'
	scope: {
		user_ids?: number[]
		subscription_ids?: number[]
		all_active?: boolean
	}
	content: { delta_mb: number; reason: string } | { subject: string; body: string }
	idempotency_key?: string
	priority?: number
	max_attempts?: number
	auto_run?: boolean
}

export async function fetchAdminTasks(params: { type?: string; status?: number; limit?: number; offset?: number } = {}): Promise<AdminTask[]> {
	const query = new URLSearchParams()
	if (params.type) query.set('type', params.type)
	if (params.status !== undefined) query.set('status', String(params.status))
	if (params.limit !== undefined) query.set('limit', String(params.limit))
	if (params.offset !== undefined) query.set('offset', String(params.offset))
	const response = await api.get(query.toString() ? `/admin/tasks?${query}` : '/admin/tasks')
	return unwrap(response) || []
}

export async function createAdminTask(payload: AdminTaskCreateRequest): Promise<AdminTask> {
	const response = await api.post('/admin/tasks', payload)
	return unwrap(response)
}

export async function runAdminTask(id: number) {
	const response = await api.post(`/admin/tasks/${id}/run`)
	return unwrap(response)
}

export type TicketStatus = 'open' | 'pending_admin' | 'pending_user' | 'resolved' | 'closed'
export type TicketCategory = 'connection' | 'billing' | 'account' | 'other'

export interface Ticket {
	id: number
	ticket_no: string
	user_id: number
	user_email: string
	subject: string
	category: TicketCategory
	priority: 1 | 2
	status: TicketStatus
	message_count: number
	last_message_at: string
	resolved_at?: string | null
	closed_at?: string | null
	created_at: string
	updated_at: string
}

export interface TicketMessage {
	id: number
	ticket_id: number
	author_id?: number | null
	author_role: 'user' | 'admin' | 'system'
	author_email: string
	type: 'message' | 'status'
	body: string
	from_status: string
	to_status: string
	created_at: string
}

export interface TicketDetail {
	ticket: Ticket
	messages: TicketMessage[]
}

export async function fetchTickets(params: { status?: string; category?: string; q?: string; offset?: number; limit?: number } = {}, admin = false) {
	const query = new URLSearchParams()
	if (params.status) query.set('status', params.status)
	if (params.category) query.set('category', params.category)
	if (params.q) query.set('q', params.q)
	if (params.offset !== undefined) query.set('offset', String(params.offset))
	if (params.limit !== undefined) query.set('limit', String(params.limit))
	const base = admin ? '/admin/tickets' : '/tickets'
	const response = await api.get(query.toString() ? `${base}?${query}` : base)
	return unwrap(response) || { items: [], total: 0, offset: 0, limit: 50 }
}

export async function fetchTicket(id: number, admin = false): Promise<TicketDetail> {
	const response = await api.get(`${admin ? '/admin' : ''}/tickets/${id}`)
	return unwrap(response)
}

export async function createTicket(payload: { subject: string; category: TicketCategory; priority: 1 | 2; body: string }): Promise<TicketDetail> {
	const response = await api.post('/tickets', payload)
	return unwrap(response)
}

export async function replyTicket(id: number, body: string, admin = false): Promise<TicketDetail> {
	const response = await api.post(`${admin ? '/admin' : ''}/tickets/${id}/messages`, { body })
	return unwrap(response)
}

export async function closeTicket(id: number): Promise<TicketDetail> {
	const response = await api.post(`/tickets/${id}/close`)
	return unwrap(response)
}

export async function updateTicketStatus(id: number, status: TicketStatus): Promise<TicketDetail> {
	const response = await api.put(`/admin/tickets/${id}/status`, { status })
	return unwrap(response)
}
