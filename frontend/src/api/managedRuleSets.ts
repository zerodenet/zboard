import axios from 'axios'
import { normalizeApiErrorPayload } from '../utils/apiError'
import { API_BASE, getAuthToken, normalizePageResult, type ApiRequestOptions, type PageResult } from './client'

export type ManagedRuleSourceFormat = 'zero_rule_ir' | 'domain_list' | 'cidr_list' | 'clash_classical'

export interface ManagedRuleSet {
  id: number
  name: string
  description: string
  renderer: 'managed' | 'znet-sink' | 'clash' | 'sing-box'
  tag: string
  url: string
  behavior: string
  format: string
  interval: number
  is_active: boolean
  revision: number
  usage_count: number
  created_at: string
  updated_at: string
  managed: boolean
  source_url?: string
  source_format?: ManagedRuleSourceFormat
  rule_count: number
  content_bytes: number
  content_sha256?: string
  public_url?: string
}

export interface ManagedRuleSetWriteRequest {
  name: string
  description: string
  tag: string
  source_url?: string
  source_format: ManagedRuleSourceFormat
  content?: string
  sync_interval: number
  is_active: boolean
  expected_revision?: number
}

export interface ManagedRuleSetContent {
  id: number
  tag: string
  content: string
  rule_count: number
  content_bytes: number
  content_sha256: string
  revision: number
}

const api = axios.create({ baseURL: API_BASE, timeout: 30_000 })
api.interceptors.request.use(config => {
  const token = getAuthToken()
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})
api.interceptors.response.use(
  response => response,
  cause => {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    return Promise.reject(cause)
  },
)

function unwrap(response: any) {
  return response.data?.data || null
}

export async function fetchManagedRuleSetsPage(
  params: { q?: string; active?: boolean; offset?: number; limit?: number },
  options: ApiRequestOptions = {},
): Promise<PageResult<ManagedRuleSet>> {
  const query = new URLSearchParams({ renderer: 'managed' })
  if (params.q) query.set('q', params.q)
  if (params.active !== undefined) query.set('active', String(params.active))
  if (params.offset !== undefined) query.set('offset', String(params.offset))
  if (params.limit !== undefined) query.set('limit', String(params.limit))
  const response = await api.get(`/admin/subscription-rule-sets?${query.toString()}`, { signal: options.signal })
  return normalizePageResult<ManagedRuleSet>(unwrap(response), params.offset || 0, params.limit || 50)
}

export async function fetchManagedRuleSet(id: number): Promise<ManagedRuleSet> {
  const response = await api.get(`/admin/subscription-rule-sets/${id}`)
  return unwrap(response)
}

export async function createManagedRuleSet(body: ManagedRuleSetWriteRequest): Promise<ManagedRuleSet> {
  const response = await api.post('/admin/subscription-rule-sets', body)
  return unwrap(response)
}

export async function updateManagedRuleSet(id: number, body: ManagedRuleSetWriteRequest): Promise<ManagedRuleSet> {
  const response = await api.put(`/admin/subscription-rule-sets/${id}`, body)
  return unwrap(response)
}

export async function deleteManagedRuleSet(id: number): Promise<void> {
  await api.delete(`/admin/subscription-rule-sets/${id}`)
}

export async function fetchManagedRuleSetContent(id: number): Promise<ManagedRuleSetContent> {
  const response = await api.get(`/admin/subscription-rule-sets/${id}/content`)
  return unwrap(response)
}

export async function updateManagedRuleSetContent(id: number, content: string, expectedRevision: number): Promise<ManagedRuleSetContent> {
  const response = await api.put(`/admin/subscription-rule-sets/${id}/content`, {
    content,
    expected_revision: expectedRevision,
  })
  return unwrap(response)
}

export async function importManagedRuleSet(
  id: number,
  sourceURL: string,
  sourceFormat: ManagedRuleSourceFormat,
  expectedRevision: number,
): Promise<ManagedRuleSet> {
  const response = await api.post(`/admin/subscription-rule-sets/${id}/import`, {
    source_url: sourceURL,
    source_format: sourceFormat,
    expected_revision: expectedRevision,
  })
  return unwrap(response)
}
