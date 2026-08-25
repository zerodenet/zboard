import axios from 'axios'
import { normalizeApiErrorPayload } from '../utils/apiError'

export const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1'

const tokenKey = 'zboard.auth.token'

const api = axios.create({
  baseURL: API_BASE,
  timeout: 30_000
})

api.interceptors.request.use((cfg) => {
  const token = localStorage.getItem(tokenKey)
  if (token) {
    cfg.headers = cfg.headers || {}
    cfg.headers.Authorization = `Bearer ${token}`
  }
  return cfg
})

api.interceptors.response.use(
  response => response,
  (cause) => {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    return Promise.reject(cause)
  },
)

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

export interface PageMeta {
  offset: number
  limit: number
  total: number
  next_cursor: string | null
  previous_cursor: string | null
}

export interface PageResult<T, A extends Record<string, unknown> = Record<string, unknown>, F extends Record<string, unknown> = Record<string, unknown>> {
  items: T[]
  page: PageMeta
  aggregates: A
  facets: F
  /** @deprecated Use page.total. */
  total: number
  /** @deprecated Use page.offset. */
  offset: number
  /** @deprecated Use page.limit. */
  limit: number
}

export interface ApiRequestOptions {
  signal?: AbortSignal
}

export function normalizePageResult<T, A extends Record<string, unknown> = Record<string, unknown>, F extends Record<string, unknown> = Record<string, unknown>>(
  data: any,
  fallbackOffset = 0,
  fallbackLimit = 50,
): PageResult<T, A, F> {
  const source = data && typeof data === 'object' ? data : {}
  const pageSource = source.page && typeof source.page === 'object' ? source.page : source
  const numberOr = (value: unknown, fallback: number) => {
    const parsed = Number(value)
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback
  }
  const page: PageMeta = {
    offset: numberOr(pageSource.offset, fallbackOffset),
    limit: Math.max(1, numberOr(pageSource.limit, fallbackLimit)),
    total: numberOr(pageSource.total, 0),
    next_cursor: typeof pageSource.next_cursor === 'string' ? pageSource.next_cursor : null,
    previous_cursor: typeof pageSource.previous_cursor === 'string' ? pageSource.previous_cursor : null,
  }
  return {
    items: Array.isArray(source.items) ? source.items : [],
    page,
    aggregates: (source.aggregates && typeof source.aggregates === 'object' ? source.aggregates : {}) as A,
    facets: (source.facets && typeof source.facets === 'object' ? source.facets : {}) as F,
    total: page.total,
    offset: page.offset,
    limit: page.limit,
  }
}

function appendPageParams(query: URLSearchParams, params: { offset?: number; limit?: number; q?: string; sort?: string; direction?: 'asc' | 'desc'; cursor?: string; from?: string; to?: string }) {
	query.set('paged', 'true')
	if (params.offset !== undefined) query.set('offset', String(params.offset))
	if (params.limit !== undefined) query.set('limit', String(params.limit))
	if (params.q) query.set('q', params.q)
	if (params.sort) query.set('sort', params.sort)
	if (params.direction) query.set('direction', params.direction)
	if (params.cursor) query.set('cursor', params.cursor)
	if (params.from) query.set('from', params.from)
	if (params.to) query.set('to', params.to)
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
	input?: {
		control: 'text' | 'textarea' | 'url' | 'email' | 'hostname' | 'password' | 'integer' | 'port' | 'switch' | 'select' | 'json'
		required?: boolean
		min?: number
		max?: number
		step?: number
		max_bytes?: number
		placeholder?: string
		options?: Array<{ label: string; value: string }>
	}
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

export type EmailTemplateCategory = 'registration' | 'operational'

export interface EmailTemplate {
	id: number
	name: string
	slug: string
	category: EmailTemplateCategory
	trigger_key?: 'user.registered'
	subject_template: string
	body_template: string
	is_active: boolean
	sort_order: number
	revision: number
	created_at: string
	updated_at: string
}

export interface EmailTemplateWriteRequest {
	name: string
	slug: string
	category: EmailTemplateCategory
	subject_template: string
	body_template: string
	is_active: boolean
	sort_order: number
	expected_revision?: number
}

export interface EmailTemplatePreview {
	subject: string
	body: string
	variables: Record<string, string>
}

export async function fetchEmailTemplates(category?: EmailTemplateCategory): Promise<EmailTemplate[]> {
	const query = category ? `?category=${encodeURIComponent(category)}` : ''
	const response = await api.get(`/admin/email-templates${query}`)
	return unwrap(response) || []
}

export async function createEmailTemplate(payload: EmailTemplateWriteRequest): Promise<EmailTemplate> {
	const response = await api.post('/admin/email-templates', payload)
	return unwrap(response)
}

export async function updateEmailTemplate(id: number, payload: EmailTemplateWriteRequest): Promise<EmailTemplate> {
	const response = await api.put(`/admin/email-templates/${id}`, payload)
	return unwrap(response)
}

export async function deleteEmailTemplate(id: number): Promise<void> {
	await api.delete(`/admin/email-templates/${id}`)
}

export async function previewEmailTemplate(payload: Pick<EmailTemplateWriteRequest, 'category' | 'subject_template' | 'body_template'>): Promise<EmailTemplatePreview> {
	const response = await api.post('/admin/email-templates/preview', payload)
	return unwrap(response)
}

export interface SMTPTestResult {
	mode: 'connection' | 'delivery'
	tls_mode: 'starttls' | 'implicit'
	authenticated: boolean
	recipient?: string
	duration_ms: number
}

export async function testSMTP(mode: 'connection' | 'delivery', recipient?: string): Promise<SMTPTestResult> {
	const response = await api.post('/admin/smtp/test', { mode, ...(recipient ? { recipient } : {}) })
	return unwrap(response)
}

export interface ProtocolKernelCapability {
	supported: boolean
	reason?: string
	minimum_zero_version?: string
}

export interface VersionInfo {
	version: string
	name: string
	zero_kernel_contract: 'legacy' | 'native-local' | 'native-local-mieru'
	zero_local_version?: string
	protocol_capabilities: Record<string, ProtocolKernelCapability>
}

export async function getVersion(): Promise<VersionInfo> {
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

export interface RegistrationCodeResult {
	sent: boolean
	expires_in: number
	resend_after: number
}

export async function requestRegistrationCode(email: string): Promise<RegistrationCodeResult> {
	const response = await api.post('/auth/register/code', { email })
	return unwrap(response)
}

export async function register(body: { email: string; password: string; verification_code?: string }) {
  const response = await api.post('/auth/register', body)
  return unwrap(response)
}

export async function fetchNodes() {
  const response = await api.get('/nodes')
  return unwrap(response) || []
}

export interface AdminNodeListItem {
	id: number
	name: string
	region: string
	address: string
	status: number
	lifecycle_status: 'active' | 'maintenance' | 'retired'
	is_enabled: boolean
	connector_last_seen_at?: string
	connector_online: boolean
	ssh_configured: boolean
	ssh_verified_at?: string
	enabled_protocol_count: number
	kernel_state?: NodeKernelState
	created_at: string
	updated_at: string
}

export interface AdminNodeDetail extends AdminNodeListItem {
	remark: string
	last_seen_at?: string
	last_sync_at?: string
	version: string
	ssh_host: string
	ssh_port: number
	ssh_user: string
	ssh_auth_method: 'password' | 'private_key'
	ssh_privilege_mode: 'none' | 'sudo' | 'su'
	ssh_privilege_password_configured: boolean
	ssh_host_key_fingerprint: string
	node_credential_prefix?: string
	node_credential_revoked_at?: string
	traffic_secret_prefix?: string
	traffic_secret_revoked_at?: string
	uptime_seconds: number
	active_flows: number
	bytes_up: number
	bytes_down: number
}

export async function fetchNodesPage(params: {
	offset?: number
	limit?: number
	q?: string
	nodeId?: number
	region?: string
	lifecycleStatus?: string
	enabled?: boolean
	connectorOnline?: boolean
	kernelStatus?: string
	sort?: string
	direction?: 'asc' | 'desc'
} = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminNodeListItem>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.nodeId) query.set('node_id', String(params.nodeId))
	if (params.region) query.set('region', params.region)
	if (params.lifecycleStatus) query.set('lifecycle_status', params.lifecycleStatus)
	if (params.enabled !== undefined) query.set('enabled', String(params.enabled))
	if (params.connectorOnline !== undefined) query.set('connector_online', String(params.connectorOnline))
	if (params.kernelStatus) query.set('kernel_status', params.kernelStatus)
	const response = await api.get(`/nodes?${query}`, { signal: options.signal })
	return normalizePageResult<AdminNodeListItem>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchNode(id: number): Promise<AdminNodeDetail> {
	const response = await api.get(`/nodes/${id}`)
	return unwrap(response)
}

export type NodeAddressCandidateSource = 'node_address' | 'node_address_dns' | 'ssh_host' | 'ssh_host_dns' | 'ssh_global'

export interface NodeAddressCandidate {
	address: string
	source: NodeAddressCandidateSource
}

export interface NodeAddressCandidates {
	node_id: number
	policy: 'public_only'
	ipv4: NodeAddressCandidate[]
	ipv6: NodeAddressCandidate[]
	recommended_ipv4?: string
	recommended_ipv6?: string
	ssh_probe_status: 'not_configured' | 'not_verified' | 'succeeded' | 'failed'
	warnings?: string[]
}

export async function fetchNodeAddressCandidates(nodeId: number, options: ApiRequestOptions = {}): Promise<NodeAddressCandidates> {
	const response = await api.get(`/admin/nodes/${nodeId}/address-candidates`, { signal: options.signal })
	return unwrap(response)
}

export interface NodeLoadSnapshot {
	node_id: number
	sampled_at: string
	cpu_core_count: number
	load_average_1: number
	load_average_5: number
	load_average_15: number
	memory_total_bytes: number
	memory_available_bytes: number
	root_total_bytes: number
	root_available_bytes: number
	uptime_seconds: number
	latency_ms: number
}

export async function fetchNodeLoad(id: number): Promise<NodeLoadSnapshot> {
	const response = await api.get(`/nodes/${id}/load`)
	return unwrap(response)
}

export async function createNode(payload: any) {
  const response = await api.post('/nodes', payload)
  return unwrap(response)
}

export async function updateNode(nodeId: number, payload: any) {
  const response = await api.put(`/nodes/${nodeId}`, payload)
  return unwrap(response)
}

export async function deleteNode(nodeId: number) {
  const response = await api.delete(`/nodes/${nodeId}`)
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
  status: 'unknown' | 'not_installed' | 'healthy' | 'degraded' | 'failed' | 'unsupported' | 'publishing' | 'apply_failed'
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

export interface ZeroReleaseOption {
  version: string
  tag: string
  published_at?: string
  prerelease: boolean
  gnu_available: boolean
  musl_available: boolean
}

export async function fetchNodeKernel(nodeId: number): Promise<{ state: NodeKernelState; operations: NodeKernelOperation[] }> {
  const response = await api.get(`/nodes/${nodeId}/kernel`)
  return unwrap(response)
}

export async function detectNodeKernel(nodeId: number) {
  const response = await api.post(`/nodes/${nodeId}/kernel/detect`)
  return unwrap(response)
}

export async function reconcileNodeKernel(nodeId: number, options: { version: string; allow_downgrade?: boolean }): Promise<AdminTask> {
  const response = await api.post(`/nodes/${nodeId}/kernel/reconcile`, options)
  return unwrap(response)
}

export async function fetchLatestZeroRelease(): Promise<ZeroRelease> {
  const response = await api.get('/admin/kernel/releases/latest')
  return unwrap(response)
}

export async function fetchZeroReleases(): Promise<ZeroReleaseOption[]> {
  const response = await api.get('/admin/kernel/releases')
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

export type ProtocolEndpointChangeEffect = 'none' | 'management' | 'billing' | 'delivery' | 'runtime' | 'credential_placement'

export interface ProtocolEndpointNodeGroupMembership {
	node_group_id: number
	name: string
	code: string
	description: string
	is_enabled: boolean
	revision: number
	sort_order: number
}

export interface ProtocolEndpointNodeGroupMembershipChange {
	node_group_id: number
	expected_revision: number
	member: boolean
}

export interface ProtocolEndpointNodeGroupMutationResult {
	added_node_group_ids?: number[]
	removed_node_group_ids?: number[]
	affected_node_ids?: number[]
	publish_status: 'queued' | 'not_required'
	reconcile_tasks?: AdminTask[]
}

export interface ProtocolEndpointMutationTiming {
	validation_ms: number
	transaction_ms: number
	task_enqueue_ms: number
	response_preparation_ms: number
	server_total_ms: number
}

export interface ProtocolEndpointMutationResult {
	protocol_endpoint: Record<string, unknown>
	effect: ProtocolEndpointChangeEffect
	effects: ProtocolEndpointChangeEffect[]
	publish_status: 'queued' | 'not_required'
	affected_node_ids?: number[]
	node_group_memberships: ProtocolEndpointNodeGroupMembership[]
	node_group_membership?: ProtocolEndpointNodeGroupMutationResult
	timing: ProtocolEndpointMutationTiming
}

export async function createProtocolEndpoint(payload: any): Promise<ProtocolEndpointMutationResult> {
  const response = await api.post('/admin/protocol-endpoints', payload)
  return unwrap(response)
}

export interface ProtocolEndpointOrderItem {
	id: number
	node_id: number
	name: string
	protocol: string
	is_active: boolean
	sort_order: number
}

export interface ProtocolEndpointOrderSnapshot {
	items: ProtocolEndpointOrderItem[]
	version: string
	total: number
}

export interface ProtocolEndpointOrderMutationResult extends ProtocolEndpointOrderSnapshot {
	effect: 'delivery'
	publish_status: 'not_required'
}

export async function fetchProtocolEndpointOrder(): Promise<ProtocolEndpointOrderSnapshot> {
	const response = await api.get('/admin/protocol-endpoints/order')
	return unwrap(response)
}

export async function updateProtocolEndpointOrder(payload: {
	ordered_ids: number[]
	expected_version: string
}): Promise<ProtocolEndpointOrderMutationResult> {
	const response = await api.put('/admin/protocol-endpoints/order', payload)
	return unwrap(response)
}

export interface RealityKeyPair {
	private_key: string
	public_key: string
	short_id: string
}

export async function generateRealityKeyPair(): Promise<RealityKeyPair> {
	const response = await api.post('/admin/protocol-endpoints/reality-keypair')
	return unwrap(response)
}

export interface RealityTemplate extends RealityKeyPair {
	preset: string
	label: string
	server_name: string
	client_fingerprint: string
}

export async function generateRealityTemplate(preset = 'compatible'): Promise<RealityTemplate> {
	const response = await api.post('/admin/protocol-endpoints/reality-template', { preset })
	return unwrap(response)
}

export async function fetchProtocolEndpoints(nodeId?: number) {
	const response = await api.get(nodeId ? `/admin/protocol-endpoints?node_id=${nodeId}` : '/admin/protocol-endpoints')
	return unwrap(response) || []
}

export interface ProtocolEndpointListItem {
	id: number
	node_id: number
	node_name: string
	name: string
	protocol: string
	address: string
	port: number
	public_port: number
	parent_protocol_id?: number
	managed_certificate_id?: number
	multiplier_milli: number
	managed_principal_ready: boolean
	mieru_principal_ready: boolean
	kernel_supported: boolean
	kernel_unsupported_reason?: string
	is_active: boolean
	sort_order: number
	usage: {
		active_flows: number
		active_users: number
		active_credentials: number
		last_used_at?: string
		used_bytes_today: number
		used_bytes_total: number
	}
	latest_deployment?: Pick<ProtocolDeployment, 'id' | 'status' | 'started_at' | 'finished_at' | 'created_at'> & { has_error: boolean }
	created_at: string
	updated_at: string
}

export async function fetchProtocolEndpointsPage(params: {
	offset?: number
	limit?: number
	q?: string
	nodeId?: number
	protocol?: string
	active?: boolean
	deploymentStatus?: string
	ids?: number[]
	sort?: string
	direction?: 'asc' | 'desc'
} = {}, options: ApiRequestOptions = {}): Promise<PageResult<ProtocolEndpointListItem>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.nodeId) query.set('node_id', String(params.nodeId))
	if (params.protocol) query.set('protocol', params.protocol)
	if (params.active !== undefined) query.set('active', String(params.active))
	if (params.deploymentStatus) query.set('deployment_status', params.deploymentStatus)
	if (params.ids?.length) query.set('ids', params.ids.join(','))
	const response = await api.get(`/admin/protocol-endpoints?${query}`, { signal: options.signal })
	return normalizePageResult<ProtocolEndpointListItem>(unwrap(response), params.offset || 0, params.limit || 50)
}

export interface ProtocolEndpointSelectionSnapshot {
	ids: number[]
	total: number
	resolved_at: string
}

export async function fetchProtocolEndpointSelection(params: {
	q?: string
	nodeId?: number
	protocol?: string
	active?: boolean
	deploymentStatus?: string
} = {}, options: ApiRequestOptions = {}): Promise<ProtocolEndpointSelectionSnapshot> {
	const query = new URLSearchParams()
	if (params.q) query.set('q', params.q)
	if (params.nodeId) query.set('node_id', String(params.nodeId))
	if (params.protocol) query.set('protocol', params.protocol)
	if (params.active !== undefined) query.set('active', String(params.active))
	if (params.deploymentStatus) query.set('deployment_status', params.deploymentStatus)
	const response = await api.get(`/admin/protocol-endpoints/selection?${query}`, { signal: options.signal })
	return unwrap(response)
}

export async function updateProtocolEndpoint(id: number, payload: any): Promise<ProtocolEndpointMutationResult> {
	const response = await api.put(`/admin/protocol-endpoints/${id}`, payload)
	return unwrap(response)
}

export async function deleteProtocolEndpoint(id: number) {
	const response = await api.delete(`/admin/protocol-endpoints/${id}`)
	return unwrap(response)
}

export async function updateProtocolEndpointMultiplier(id: number, multiplierMilli: number) {
	const response = await api.patch(`/admin/protocol-endpoints/${id}/multiplier`, { multiplier_milli: multiplierMilli })
	return unwrap(response)
}

export async function deployProtocolEndpoint(id: number) {
	const response = await api.post(`/admin/protocol-endpoints/${id}/deploy`, undefined, { timeout: 120_000 })
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

export interface CertificateOperation {
	id: number
	managed_certificate_id: number
	node_id: number
	operation_type: 'issue' | 'renew'
	status: 'running' | 'succeeded' | 'failed'
	phase: string
	result_summary: string
	error: string
	started_at?: string
	finished_at?: string
	created_at: string
}

export interface ManagedCertificate {
	id: number
	node_id: number
	provider_account_id?: number
	node_name: string
	name: string
	domains: string[]
	contact_email: string
	environment: 'production' | 'staging'
	challenge_type: 'http-01' | 'http-01-webroot' | 'dns-01'
	webroot_path: string
	status: 'pending' | 'issuing' | 'active' | 'renewing' | 'failed' | 'expired'
	cert_path: string
	key_path: string
	serial_number: string
	fingerprint_sha256: string
	not_before?: string
	not_after?: string
	last_issued_at?: string
	last_renewal_attempt_at?: string
	next_renewal_at?: string
	auto_renew: boolean
	renew_before_days: number
	last_error: string
	revision: number
	usage_count: number
	latest_operation?: CertificateOperation
	created_at: string
	updated_at: string
}

export async function fetchManagedCertificatesPage(params: {
	offset?: number
	limit?: number
	q?: string
	nodeId?: number
	status?: string
} = {}, options: ApiRequestOptions = {}): Promise<PageResult<ManagedCertificate>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.nodeId) query.set('node_id', String(params.nodeId))
	if (params.status) query.set('status', params.status)
	const response = await api.get(`/admin/certificates?${query}`, { signal: options.signal })
	return normalizePageResult<ManagedCertificate>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function createManagedCertificate(payload: {
	node_id: number
	provider_account_id: number
	name: string
	domains: string[]
	contact_email: string
	environment: 'production' | 'staging'
	auto_renew: boolean
	renew_before_days: number
	challenge_type: 'http-01-webroot' | 'dns-01'
	webroot_path: string
}): Promise<ManagedCertificate> {
	const response = await api.post('/admin/certificates', payload)
	return unwrap(response)
}

export async function issueManagedCertificate(id: number): Promise<CertificateOperation> {
	const response = await api.post(`/admin/certificates/${id}/issue`)
	return unwrap(response)
}

export async function renewManagedCertificate(id: number): Promise<CertificateOperation> {
	const response = await api.post(`/admin/certificates/${id}/renew`)
	return unwrap(response)
}

export async function updateManagedCertificateRenewal(id: number, payload: {
	auto_renew: boolean
	renew_before_days: number
	expected_revision: number
}) {
	const response = await api.put(`/admin/certificates/${id}/renewal`, payload)
	return unwrap(response)
}

export async function updateManagedCertificate(id: number, payload: {
	name: string
	contact_email: string
	webroot_path: string
	auto_renew: boolean
	renew_before_days: number
	expected_revision: number
}) {
	return unwrap(await api.put(`/admin/certificates/${id}`, payload))
}

export async function deleteManagedCertificate(id: number) {
	const response = await api.delete(`/admin/certificates/${id}`)
	return unwrap(response)
}

export interface ProviderDefinition {
	key: string
	name: string
	capabilities: string[]
}

export interface ProviderAccount {
	id: number
	provider_key: string
	name: string
	capabilities: string[]
	credential_prefix: string
	status: 'pending' | 'active' | 'invalid' | 'disabled'
	last_verified_at?: string
	last_error: string
	revision: number
	usage_count: number
	created_at: string
	updated_at: string
}

export interface ProviderOperation {
	id: number
	provider_account_id: number
	resource_type: string
	resource_id: number
	operation_type: string
	status: 'running' | 'succeeded' | 'failed'
	phase: string
	result_summary: string
	error: string
	created_at: string
}

export interface ManagedDNSRecord {
	id: number
	provider_account_id: number
	provider_name: string
	provider_key: string
	node_id: number
	node_name: string
	domain_name: string
	record_type: 'A' | 'AAAA'
	record_value: string
	provider_zone_id: string
	provider_record_id: string
	ttl: number
	proxied: boolean
	status: 'pending' | 'syncing' | 'active' | 'drifted' | 'failed'
	public_resolved: boolean
	last_synced_at?: string
	last_public_check_at?: string
	last_error: string
	revision: number
	latest_operation?: ProviderOperation
	created_at: string
	updated_at: string
}

export async function fetchProviderDefinitions(): Promise<ProviderDefinition[]> {
	return unwrap(await api.get('/admin/provider-definitions'))
}

export async function fetchProviderAccounts(): Promise<ProviderAccount[]> {
	return unwrap(await api.get('/admin/provider-accounts'))
}

export async function createProviderAccount(payload: { provider_key: string; name: string; api_token: string }): Promise<ProviderAccount> {
	return unwrap(await api.post('/admin/provider-accounts', payload))
}

export async function verifyProviderAccount(id: number): Promise<ProviderAccount> {
	return unwrap(await api.post(`/admin/provider-accounts/${id}/verify`))
}

export async function fetchManagedDNSRecordsPage(params: { offset?: number; limit?: number; q?: string; status?: string } = {}, options: ApiRequestOptions = {}): Promise<PageResult<ManagedDNSRecord>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.status) query.set('status', params.status)
	return normalizePageResult<ManagedDNSRecord>(unwrap(await api.get(`/admin/dns-records?${query}`, { signal: options.signal })), params.offset || 0, params.limit || 50)
}

export async function createManagedDNSRecord(payload: {
	provider_account_id: number
	node_id: number
	domain_name: string
	records: Array<{ record_type: 'A' | 'AAAA'; record_value: string }>
	ttl: number
	proxied: boolean
	takeover_existing: boolean
}) {
	return unwrap(await api.post('/admin/dns-records', payload))
}

export async function updateManagedDNSRecord(id: number, payload: {
	provider_account_id: number
	node_id: number
	domain_name: string
	record_type: 'A' | 'AAAA'
	record_value: string
	ttl: number
	proxied: boolean
	expected_revision: number
}): Promise<ProviderOperation> {
	return unwrap(await api.put(`/admin/dns-records/${id}`, payload))
}

export async function deleteManagedDNSRecord(id: number): Promise<{ id: number; deleted: boolean; remote_record_deleted: boolean }> {
	return unwrap(await api.delete(`/admin/dns-records/${id}`))
}

export async function syncManagedDNSRecord(id: number, takeover = false): Promise<ProviderOperation> {
	return unwrap(await api.post(`/admin/dns-records/${id}/sync${takeover ? '?takeover=true' : ''}`))
}

export async function fetchProtocolDeployments(params: { nodeId?: number; protocolEndpointId?: number; status?: string; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}) {
  const query = new URLSearchParams()
  if (params.nodeId) query.set('node_id', String(params.nodeId))
  if (params.protocolEndpointId) query.set('protocol_endpoint_id', String(params.protocolEndpointId))
  if (params.status) query.set('status', params.status)
  if (params.offset !== undefined) query.set('offset', String(params.offset))
  if (params.limit !== undefined) query.set('limit', String(params.limit))
  const path = query.toString() ? `/admin/protocol-deployments?${query}` : '/admin/protocol-deployments'
  const response = await api.get(path, { signal: options.signal })
  return normalizePageResult<any>(unwrap(response), 0, params.limit || 50)
}

export async function fetchNodeGroups() {
	const response = await api.get('/admin/node-groups')
	return unwrap(response) || []
}

export interface NodeGroupSummary {
	id: number
	name: string
	code: string
	description: string
	is_enabled: boolean
	revision: number
	protocol_endpoint_count: number
	plan_count: number
	created_at: string
	updated_at: string
}

export interface NodeGroupDetail extends NodeGroupSummary {
	protocol_endpoint_ids: number[]
}

export interface NodeGroupMutationResponse extends NodeGroupDetail {
	reconcile_task?: AdminTask
}

export async function fetchNodeGroupsPage(params: { offset?: number; limit?: number; q?: string; groupId?: number; enabled?: boolean } = {}, options: ApiRequestOptions = {}): Promise<PageResult<NodeGroupSummary>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.groupId) query.set('group_id', String(params.groupId))
	if (params.enabled !== undefined) query.set('enabled', String(params.enabled))
	const response = await api.get(`/admin/node-groups?${query}`, { signal: options.signal })
	return normalizePageResult<NodeGroupSummary>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchNodeGroupDetail(id: number, options: ApiRequestOptions = {}): Promise<NodeGroupDetail> {
	const response = await api.get(`/admin/node-groups/${id}`, { signal: options.signal })
	return unwrap(response)
}

export async function createNodeGroup(payload: any): Promise<NodeGroupMutationResponse> {
	const response = await api.post('/admin/node-groups', payload)
	return unwrap(response)
}

export async function updateNodeGroup(id: number, payload: any): Promise<NodeGroupMutationResponse> {
	const response = await api.put(`/admin/node-groups/${id}`, payload)
	return unwrap(response)
}

export interface PlanNodeGroupSummary {
  id: number
  name: string
  code: string
  is_enabled: boolean
}

export interface PlanSKU {
  id: number
  plan_id: number
  code: string
  name: string
  sku_type: string
  billing_mode?: 'periodic' | 'one_time'
  allowed_operations?: Array<'purchase' | 'renew' | 'change' | 'addon'>
  billing_unit: string
  billing_value: number
  price_cents: number
  currency: string
  grant_traffic_bytes: number
  is_active: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface PlanSummary {
  id: number
  name: string
  slug: string
  summary: string
  node_group_id: number
  node_group?: PlanNodeGroupSummary
  is_active: boolean
  sort_order: number
  revision: number
  sku_count: number
  active_sku_count: number
  created_at: string
  updated_at: string
}

export interface PlanDetail extends PlanSummary {
  description: string
  traffic_bytes: number
  speed_limit_mbps: number
  max_active_subscriptions: number
  is_renewable: boolean
  device_limit: number
  family_limit: number
  reset_policy: number
  traffic_calc_mode: number
}

export interface PlanCatalogItem extends PlanSummary {
  traffic_bytes: number
  speed_limit_mbps: number
  device_limit: number
  primary_sku?: PlanSKU
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

export async function fetchPlansPage(params: { includeInactive?: boolean; q?: string; active?: boolean; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<PlanSummary>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.includeInactive) query.set('include_inactive', 'true')
  if (params.active !== undefined) query.set('active', String(params.active))
  const response = await api.get(`/plans?${query}`, { signal: options.signal })
  return normalizePageResult<PlanSummary>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchPlanCatalogPage(params: { q?: string; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<PlanCatalogItem>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  const response = await api.get(`/plans?${query}`, { signal: options.signal })
  return normalizePageResult<PlanCatalogItem>(unwrap(response), params.offset || 0, params.limit || 25)
}

export async function fetchPlanCatalogItem(id: number, options: ApiRequestOptions = {}): Promise<PlanCatalogItem> {
  const response = await api.get(`/plans/${id}`, { signal: options.signal })
  return unwrap(response)
}

export async function fetchPlanCatalogSKUs(planId: number, params: { q?: string; skuType?: PlanSKU['sku_type']; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<PlanSKU>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.skuType) query.set('sku_type', params.skuType)
  const response = await api.get(`/plans/${planId}/skus?${query}`, { signal: options.signal })
  return normalizePageResult<PlanSKU>(unwrap(response), params.offset || 0, params.limit || 25)
}

export async function fetchPlanDetail(id: number, options: ApiRequestOptions = {}): Promise<PlanDetail> {
  const response = await api.get(`/admin/plans/${id}`, { signal: options.signal })
  return unwrap(response)
}

export async function fetchPlanSKUs(planId: number, params: { q?: string; active?: boolean; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<PlanSKU>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.active !== undefined) query.set('active', String(params.active))
  const response = await api.get(`/admin/plans/${planId}/skus?${query}`, { signal: options.signal })
  return normalizePageResult<PlanSKU>(unwrap(response), params.offset || 0, params.limit || 25)
}

export async function fetchPlanSKU(id: number, options: ApiRequestOptions = {}): Promise<PlanSKU> {
  const response = await api.get(`/admin/plan-skus/${id}`, { signal: options.signal })
  return unwrap(response)
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

export interface AdminOrderListItem {
  id: number
  user_id: number
  subscription_id: number
  plan_id: number
  plan_sku_id: number
  trade_no: string
  order_type: string
  amount_cents: number
  currency: string
  status: string
  plan_name: string
  sku_name: string
  created_at: string
  updated_at: string
}

export interface AdminOrderDetail extends AdminOrderListItem {
  target_subscription_id?: number | null
  payable_amount: number
  paid_amount: number
  refund_amount: number
  discount_amount: number
  channel: string
  provider_trade_no?: string | null
  billing_unit: string
  billing_value: number
  traffic_bytes: number
  device_limit: number
  speed_limit_mbps: number
  paid_at?: string | null
  canceled_at?: string | null
  fulfilled_at?: string | null
  refunded_at?: string | null
  failure_reason: string
}

export interface AdminPaymentEventSummary {
  id: number
  provider: string
  provider_event_id: string
  event_type: string
  amount_minor: number
  signature_valid: boolean
  processed_at?: string | null
  created_at: string
}

export async function fetchOrdersPage(params: { q?: string; status?: string; orderType?: string; userId?: number; createdFrom?: string; createdTo?: string; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminOrderListItem>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.status) query.set('status', params.status)
  if (params.orderType) query.set('order_type', params.orderType)
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.createdFrom) query.set('created_from', params.createdFrom)
  if (params.createdTo) query.set('created_to', params.createdTo)
  const response = await api.get(`/admin/orders?${query}`, { signal: options.signal })
  return normalizePageResult<AdminOrderListItem>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchAccountOrdersPage(params: { status?: string; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminOrderListItem>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.status) query.set('status', params.status)
  const response = await api.get(`/orders?${query}`, { signal: options.signal })
  return normalizePageResult<AdminOrderListItem>(unwrap(response), params.offset || 0, params.limit || 25)
}

export async function fetchAdminOrderDetail(id: number, options: ApiRequestOptions = {}): Promise<AdminOrderDetail> {
  const response = await api.get(`/admin/orders/${id}`, { signal: options.signal })
  return unwrap(response)
}

export async function fetchAdminOrderPaymentEvents(orderId: number, params: { offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminPaymentEventSummary>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  const response = await api.get(`/admin/orders/${orderId}/payment-events?${query}`, { signal: options.signal })
  return normalizePageResult<AdminPaymentEventSummary>(unwrap(response), params.offset || 0, params.limit || 50)
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

export interface AdminSubscriptionListItem {
  id: number
  user_id: number
  user_email: string
  plan_id: number
  plan_name: string
  plan_sku_id: number
  sku_name: string
  node_group_id: number
  start_at: string
  end_at: string
  status: string
  flow_total: number
  flow_used: number
  speed_limit_mbps: number
  device_limit: number
  family_limit: number
  created_at: string
  updated_at: string
}

export interface AdminSubscriptionDetail extends AdminSubscriptionListItem {
  subscription_type: number
  renewal_price_minor: number
  reset_policy: number
  next_reset_at?: string | null
  traffic_calc_mode: number
  active_credential_count: number
  total_credential_count: number
}

export async function fetchSubscriptionsPage(params: { q?: string; userId?: number; status?: string; quota?: string; expiresFrom?: string; expiresTo?: string; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminSubscriptionListItem>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.status) query.set('status', params.status)
  if (params.quota) query.set('quota', params.quota)
  if (params.expiresFrom) query.set('expires_from', params.expiresFrom)
  if (params.expiresTo) query.set('expires_to', params.expiresTo)
  const response = await api.get(`/admin/subscriptions?${query}`, { signal: options.signal })
  return normalizePageResult<AdminSubscriptionListItem>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchAccountSubscriptionsPage(params: { status?: string; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminSubscriptionListItem>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.status) query.set('status', String(params.status))
  const response = await api.get(`/subscriptions?${query}`, { signal: options.signal })
  return normalizePageResult<AdminSubscriptionListItem>(unwrap(response), params.offset || 0, params.limit || 25)
}

export async function fetchAdminSubscriptionDetail(id: number, options: ApiRequestOptions = {}): Promise<AdminSubscriptionDetail> {
  const response = await api.get(`/admin/subscriptions/${id}`, { signal: options.signal })
  return unwrap(response)
}

export async function fetchSubscriptionAccess() {
  const response = await api.get('/subscription/access')
  return unwrap(response) || { configured: false }
}

export interface AccountProtocolLoadItem {
	protocol_endpoint_id: number
	name: string
	region: string
	protocol: string
	active_users: number
	active_flows: number
	last_activity_at?: string
}

export interface AccountProtocolLoadSnapshot {
	sampled_at: string
	activity_window_seconds: number
	items: AccountProtocolLoadItem[]
}

export async function fetchAccountProtocolLoads(): Promise<AccountProtocolLoadSnapshot> {
	const response = await api.get('/subscription/protocol-loads')
	return unwrap(response)
}

export async function rotateSubscriptionAccess() {
  const response = await api.post('/subscription/access/rotate')
  return unwrap(response)
}

export async function revokeSubscriptionAccess() {
  const response = await api.delete('/subscription/access')
  return unwrap(response)
}

export type SubscriptionRenderer = 'znet-sink' | 'clash' | 'sing-box' | 'unsupported'

export type SubscriptionPolicyGroupType = 'select' | 'urltest' | 'fallback'

export interface SubscriptionPolicyGroup {
	id: string
	name: string
	type: SubscriptionPolicyGroupType
	include_pattern?: string
	exclude_pattern?: string
	include_groups?: string[]
	default_group?: string
	probe_url?: string
	interval?: number
	tolerance?: number
}

export interface SubscriptionTemplateRuleSet {
	rule_set_id?: number
	tag?: string
	url?: string
	behavior?: 'domain' | 'ipcidr' | 'classical'
	format?: 'domain_list' | 'cidr_list' | 'zero_rule_ir' | 'zrs' | 'yaml' | 'text' | 'mrs' | 'source' | 'binary'
	target: string
	interval?: number
}

export interface SubscriptionRuleSet {
	id: number
	name: string
	description: string
	renderer: Exclude<SubscriptionRenderer, 'unsupported'>
	tag: string
	url: string
	behavior?: 'domain' | 'ipcidr' | 'classical'
	format: NonNullable<SubscriptionTemplateRuleSet['format']>
	interval: number
	is_active: boolean
	revision: number
	usage_count: number
	created_at: string
	updated_at: string
}

export type SubscriptionRuleSetWriteRequest = Omit<SubscriptionRuleSet, 'id' | 'revision' | 'usage_count' | 'created_at' | 'updated_at'> & {
	expected_revision?: number
}

export interface SubscriptionTemplateCustomization {
	version: 2
	mixed_port: number
	main_group: string
	policy_groups: SubscriptionPolicyGroup[]
	final: string
	rule_sets: SubscriptionTemplateRuleSet[]
	advanced_source?: string
}

export interface SubscriptionTemplate {
	id: number
	name: string
	slug: string
	description: string
	renderer: SubscriptionRenderer
	customization?: SubscriptionTemplateCustomization
	content_type?: 'text/plain' | 'text/yaml' | 'application/yaml' | 'application/json'
	is_active: boolean
	sort_order: number
	revision: number
	created_at: string
	updated_at: string
}

export type SubscriptionTemplateWriteRequest = Omit<SubscriptionTemplate, 'id' | 'content_type' | 'revision' | 'created_at' | 'updated_at'> & {
	expected_revision?: number
}

export type SubscriptionTemplatePreview = {
	content: string
	content_type: SubscriptionTemplate['content_type']
	bytes: number
	line_count: number
	truncated: boolean
}

export async function fetchSubscriptionRuleSetsPage(params: {
	offset?: number
	limit?: number
	q?: string
	renderer?: Exclude<SubscriptionRenderer, 'unsupported'>
	active?: boolean
	id?: number
	ids?: number[]
} = {}, options: ApiRequestOptions = {}): Promise<PageResult<SubscriptionRuleSet>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.renderer) query.set('renderer', params.renderer)
	if (params.active !== undefined) query.set('active', String(params.active))
	if (params.id) query.set('id', String(params.id))
	if (params.ids?.length) query.set('ids', params.ids.join(','))
	const response = await api.get(`/admin/subscription-rule-sets?${query}`, { signal: options.signal })
	return normalizePageResult<SubscriptionRuleSet>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchSubscriptionRuleSet(id: number): Promise<SubscriptionRuleSet> {
	const response = await api.get(`/admin/subscription-rule-sets/${id}`)
	return unwrap(response)
}

export async function createSubscriptionRuleSet(payload: SubscriptionRuleSetWriteRequest): Promise<SubscriptionRuleSet> {
	const response = await api.post('/admin/subscription-rule-sets', payload)
	return unwrap(response)
}

export async function updateSubscriptionRuleSet(id: number, payload: SubscriptionRuleSetWriteRequest): Promise<SubscriptionRuleSet> {
	const response = await api.put(`/admin/subscription-rule-sets/${id}`, payload)
	return unwrap(response)
}

export async function deleteSubscriptionRuleSet(id: number) {
	const response = await api.delete(`/admin/subscription-rule-sets/${id}`)
	return unwrap(response)
}

export async function fetchSubscriptionTemplates(admin = false): Promise<SubscriptionTemplate[]> {
	const response = await api.get(`${admin ? '/admin' : ''}/subscription-templates`)
	return unwrap(response) || []
}

export async function fetchActiveSubscriptionTemplatesPage(params: {
	q?: string
	offset?: number
	limit?: number
} = {}, options: ApiRequestOptions = {}): Promise<PageResult<SubscriptionTemplate>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	const response = await api.get(`/subscription-templates?${query}`, { signal: options.signal })
	return normalizePageResult<SubscriptionTemplate>(unwrap(response), params.offset || 0, params.limit || 25)
}

export async function fetchSubscriptionTemplatesPage(params: {
	offset?: number
	limit?: number
	q?: string
	active?: boolean
} = {}, options: ApiRequestOptions = {}): Promise<PageResult<SubscriptionTemplate>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.active !== undefined) query.set('active', String(params.active))
	const response = await api.get(`/admin/subscription-templates?${query}`, { signal: options.signal })
	return normalizePageResult<SubscriptionTemplate>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchSubscriptionTemplate(id: number): Promise<SubscriptionTemplate> {
	const response = await api.get(`/admin/subscription-templates/${id}`)
	return unwrap(response)
}

export async function createSubscriptionTemplate(payload: SubscriptionTemplateWriteRequest): Promise<SubscriptionTemplate> {
	const response = await api.post('/admin/subscription-templates', payload)
	return unwrap(response)
}

export async function previewSubscriptionTemplate(payload: Pick<SubscriptionTemplateWriteRequest, 'renderer' | 'customization'>): Promise<SubscriptionTemplatePreview> {
	const response = await api.post('/admin/subscription-templates/preview', payload)
	return unwrap(response)
}

export async function updateSubscriptionTemplate(id: number, payload: SubscriptionTemplateWriteRequest): Promise<SubscriptionTemplate> {
	const response = await api.put(`/admin/subscription-templates/${id}`, payload)
	return unwrap(response)
}

export async function deleteSubscriptionTemplate(id: number) {
	const response = await api.delete(`/admin/subscription-templates/${id}`)
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

export type TrafficRecordSummary = {
  id: number
  user_id: number
  subscription_id?: number
  node_id: number
  protocol_endpoint_id: number
  raw_bytes: number
  upload_bytes: number
  download_bytes: number
  protocol_multiplier_milli: number
  used_bytes: number
  record_at: string
}

export type TrafficRecordAggregates = {
  raw_bytes: number
  used_bytes: number
  user_count: number
  subscription_count: number
  node_count: number
  protocol_endpoint_count: number
}

export async function fetchTrafficRecordsPage(params: { userId?: number; nodeId?: number; protocolEndpointId?: number; subscriptionId?: number; cursor?: string; from?: string; to?: string; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<TrafficRecordSummary, TrafficRecordAggregates>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.nodeId) query.set('node_id', String(params.nodeId))
  if (params.protocolEndpointId) query.set('protocol_endpoint_id', String(params.protocolEndpointId))
  if (params.subscriptionId) query.set('subscription_id', String(params.subscriptionId))
  const response = await api.get(`/admin/traffic/records?${query}`, { signal: options.signal })
  return normalizePageResult<TrafficRecordSummary, TrafficRecordAggregates>(unwrap(response), 0, params.limit || 50)
}

export async function fetchAccountTrafficRecordsPage(params: { subscriptionId?: number; cursor?: string; from?: string; to?: string; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<TrafficRecordSummary, TrafficRecordAggregates>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.subscriptionId) query.set('subscription_id', String(params.subscriptionId))
  const response = await api.get(`/traffic/records?${query}`, { signal: options.signal })
  return normalizePageResult<TrafficRecordSummary, TrafficRecordAggregates>(unwrap(response), 0, params.limit || 25)
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

export type TrafficReconciliationItem = {
  subscription_id: number
  user_id: number
  plan_id: number
  status: string
  flow_used: number
  recorded_bytes: number
  difference: number
  result: 'matched' | 'missing_records' | 'over_recorded'
}

export type TrafficReconciliationAggregates = {
  subscription_count: number
  matched_count: number
  missing_records_count: number
  over_recorded_count: number
  flow_used: number
  recorded_bytes: number
  missing_bytes: number
  over_recorded_bytes: number
}

export async function fetchTrafficReconciliationPage(params: { userId?: number; subscriptionId?: number; issuesOnly?: boolean; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<TrafficReconciliationItem, TrafficReconciliationAggregates>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.subscriptionId) query.set('subscription_id', String(params.subscriptionId))
  if (params.issuesOnly) query.set('issues_only', 'true')
  const response = await api.get(`/admin/traffic/reconciliation?${query}`, { signal: options.signal })
  return normalizePageResult<TrafficReconciliationItem, TrafficReconciliationAggregates>(unwrap(response), params.offset || 0, params.limit || 25)
}

export async function fetchDashboard() {
  const response = await api.get('/admin/dashboard')
  return unwrap(response) || {}
}

export interface AuditLogSummary {
  id: number
  actor: string
  action: string
  target: string
  has_detail: boolean
  created_at: string
}

export interface AuditLogDetail extends AuditLogSummary {
  user_id?: number
  detail?: string
}

export async function fetchAuditLogs(params: { actor?: string; action?: string; target?: string; cursor?: string; from?: string; to?: string; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AuditLogSummary>> {
  const query = new URLSearchParams()
  if (params.actor) query.set('actor', params.actor)
  if (params.action) query.set('action', params.action)
  if (params.target) query.set('target', params.target)
  if (params.cursor) query.set('cursor', params.cursor)
  if (params.from) query.set('from', params.from)
  if (params.to) query.set('to', params.to)
  if (params.limit !== undefined) query.set('limit', String(params.limit))
  const path = query.toString() ? `/admin/audit-logs?${query}` : '/admin/audit-logs'
  const response = await api.get(path, { signal: options.signal })
  return normalizePageResult<AuditLogSummary>(unwrap(response), 0, params.limit || 50)
}

export async function fetchAuditLog(id: number, options: ApiRequestOptions = {}): Promise<AuditLogDetail> {
  const response = await api.get(`/admin/audit-logs/${id}`, { signal: options.signal })
  return unwrap(response)
}

export interface OperationLog {
	id: number
	source: 'protocol_publish' | 'node_kernel' | 'task'
	action: string
	status: 'queued' | 'running' | 'succeeded' | 'failed'
	target_type: string
	target_id: number
	node_id?: number
	protocol_endpoint_id?: number
	summary?: string
	has_output?: boolean
	has_error?: boolean
	output?: string
	error?: string
	started_at?: string
	finished_at?: string
	created_at: string
}

export async function fetchOperationLog(source: OperationLog['source'], id: number, options: ApiRequestOptions = {}): Promise<OperationLog> {
	const response = await api.get(`/admin/operation-logs/${source}/${id}`, { signal: options.signal })
	return unwrap(response)
}

export async function fetchOperationLogs(params: { source?: string; status?: string; nodeId?: number; protocolEndpointId?: number; cursor?: string; from?: string; to?: string; limit?: number } = {}, options: ApiRequestOptions = {}) {
	const query = new URLSearchParams()
	if (params.source) query.set('source', params.source)
	if (params.status) query.set('status', params.status)
	if (params.nodeId) query.set('node_id', String(params.nodeId))
	if (params.protocolEndpointId) query.set('protocol_endpoint_id', String(params.protocolEndpointId))
	if (params.cursor) query.set('cursor', params.cursor)
	if (params.from) query.set('from', params.from)
	if (params.to) query.set('to', params.to)
	if (params.limit !== undefined) query.set('limit', String(params.limit))
	const response = await api.get(query.toString() ? `/admin/operation-logs?${query}` : '/admin/operation-logs', { signal: options.signal })
	return normalizePageResult<OperationLog>(unwrap(response), 0, params.limit || 50)
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

export interface AdminUserListItem {
  id: number
  email: string
  is_admin: boolean
  status: string
  active_subscription_count: number
  total_subscription_count: number
  pending_order_count: number
  total_order_count: number
  created_at: string
}

export interface AdminUserDetail extends AdminUserListItem {
  account_name: string
  email_verified_at?: string | null
  last_login_at?: string | null
  active_subscription_count: number
  total_subscription_count: number
  pending_order_count: number
  total_order_count: number
  created_at: string
  updated_at: string
}

export async function fetchUsersPage(params: { q?: string; status?: string; isAdmin?: boolean; sort?: 'id' | 'email' | 'created_at'; direction?: 'asc' | 'desc'; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminUserListItem>> {
  const query = new URLSearchParams()
  appendPageParams(query, params)
  if (params.status) query.set('status', params.status)
  if (params.isAdmin !== undefined) query.set('is_admin', String(params.isAdmin))
  if (params.sort) query.set('sort', params.sort)
  if (params.direction) query.set('direction', params.direction)
  const response = await api.get(`/admin/users?${query}`, { signal: options.signal })
  return normalizePageResult<AdminUserListItem>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchAdminUserDetail(id: number, options: ApiRequestOptions = {}): Promise<AdminUserDetail> {
  const response = await api.get(`/admin/users/${id}`, { signal: options.signal })
  return unwrap(response)
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
	type: 'quota' | 'email' | 'node_detect' | 'node_reconcile' | 'node_lifecycle' | 'protocol_deploy' | 'protocol_active' | 'node_group_reconcile'
	scope: string
	content: string
	status: number
	errors: string
	total: number
	current: number
	pending_count?: number
	running_count?: number
	succeeded_count?: number
	failed_count?: number
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

export interface AdminTaskSummary {
	total: number
	pending: number
	running: number
	completed: number
	failed: number
	active_current: number
	active_total: number
	pending_targets: number
	running_targets: number
	succeeded_targets: number
	failed_targets: number
}

export interface AdminTaskItem {
	id: number
	task_id: number
	target_type: string
	target_id: string
	payload: string
	status: number
	attempts: number
	error: string
	started_at?: string
	finished_at?: string
	created_at: string
	updated_at: string
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

export async function fetchAdminTasksPage(params: { type?: string; status?: number; limit?: number; offset?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminTask>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.type) query.set('type', params.type)
	if (params.status !== undefined) query.set('status', String(params.status))
	const response = await api.get(`/admin/tasks?${query}`, { signal: options.signal })
	return normalizePageResult<AdminTask>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchAdminTaskSummary(): Promise<AdminTaskSummary> {
	const response = await api.get('/admin/tasks/summary')
	return unwrap(response)
}

export async function createAdminTask(payload: AdminTaskCreateRequest): Promise<AdminTask> {
	const response = await api.post('/admin/tasks', payload)
	return unwrap(response)
}

export async function runAdminTask(id: number) {
	const response = await api.post(`/admin/tasks/${id}/run`)
	return unwrap(response)
}

export async function fetchAdminTask(id: number, summary = true): Promise<AdminTask> {
	const response = await api.get(`/admin/tasks/${id}${summary ? '?summary=true' : ''}`)
	return unwrap(response)
}

export async function fetchAdminTaskItems(id: number, params: { status?: number; offset?: number; limit?: number } = {}, options: ApiRequestOptions = {}): Promise<PageResult<AdminTaskItem>> {
	const query = new URLSearchParams()
	appendPageParams(query, params)
	if (params.status !== undefined) query.set('status', String(params.status))
	const response = await api.get(`/admin/tasks/${id}/items?${query}`, { signal: options.signal })
	return normalizePageResult<AdminTaskItem>(unwrap(response), params.offset || 0, params.limit || 50)
}

export interface NodeBatchFilters {
	q?: string
	lifecycle_status?: string
	connector_online?: boolean
	enabled?: boolean
	kernel_status?: string
}

export async function createNodeBatchOperation(payload: {
	action: 'detect' | 'reconcile' | 'activate' | 'maintenance' | 'retire'
	node_ids?: number[]
	all_matching?: boolean
	filters?: NodeBatchFilters
	version?: string
	allow_downgrade?: boolean
	idempotency_key?: string
}): Promise<AdminTask> {
	const response = await api.post('/admin/node-operations', payload)
	return unwrap(response)
}

export interface ProtocolBatchFilters {
	q?: string
	node_id?: number
	protocol?: string
	active?: boolean
	deployment_status?: string
}

export async function createProtocolBatchDeployment(payload: {
	protocol_endpoint_ids?: number[]
	all_matching?: boolean
	filters?: ProtocolBatchFilters
	idempotency_key?: string
}): Promise<AdminTask> {
	const response = await api.post('/admin/protocol-deployments/batch', payload)
	return unwrap(response)
}

export async function updateProtocolEndpointsBatch(payload: {
	is_active: boolean
	protocol_endpoint_ids?: number[]
	all_matching?: boolean
	filters?: ProtocolBatchFilters
	idempotency_key?: string
}): Promise<AdminTask> {
	const response = await api.patch('/admin/protocol-endpoints/batch', payload)
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
	has_older_messages: boolean
	oldest_message_id?: number
}

export async function fetchTickets(params: { status?: string; category?: string; q?: string; offset?: number; limit?: number } = {}, admin = false, options: ApiRequestOptions = {}) {
	const query = new URLSearchParams()
	if (params.status) query.set('status', params.status)
	if (params.category) query.set('category', params.category)
	if (params.q) query.set('q', params.q)
	if (params.offset !== undefined) query.set('offset', String(params.offset))
	if (params.limit !== undefined) query.set('limit', String(params.limit))
	const base = admin ? '/admin/tickets' : '/tickets'
	const response = await api.get(query.toString() ? `${base}?${query}` : base, { signal: options.signal })
	return normalizePageResult<Ticket>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchTicket(id: number, admin = false, options: ApiRequestOptions & { beforeId?: number; messageLimit?: number } = {}): Promise<TicketDetail> {
	const query = new URLSearchParams()
	if (options.beforeId) query.set('before_id', String(options.beforeId))
	if (options.messageLimit) query.set('message_limit', String(options.messageLimit))
	const path = `${admin ? '/admin' : ''}/tickets/${id}${query.toString() ? `?${query}` : ''}`
	const response = await api.get(path, { signal: options.signal })
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
