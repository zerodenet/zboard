import type { SubscriptionTemplateCustomization } from '../api/client'

const record = (v: unknown): v is Record<string, any> => Boolean(v) && typeof v === 'object' && !Array.isArray(v)
const text = (v: unknown, max = 4096): v is string => typeof v === 'string' && new TextEncoder().encode(v).byteLength <= max
const int = (v: unknown): v is number => Number.isSafeInteger(v)
const keys = (v: Record<string, any>, allowed: string[]) => Object.keys(v).every(key => allowed.includes(key))
const texts = (v: unknown, max: number, length: number): v is string[] => Array.isArray(v) && v.length <= length && v.every(item => text(item, max))
const renderers = ['clash', 'sing-box', 'zero', 'znet-sink', 'shadowrocket', 'quantumult-x', 'v2rayn', 'unsupported']
// URLs can be public ordinary configuration, but authentication/query/fragment
// components are not draft content. Unchanged server URLs use a null reference.
export function publicSubscriptionDraftURL(v: unknown): v is string {
  if (!text(v, 8192)) return false
  if (!v) return true
  try { const url = new URL(v); return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password && !url.search && !url.hash }
  catch { return false }
}
function capturedURL(v: unknown, original: unknown) { return typeof v === 'string' && v === original ? null : publicSubscriptionDraftURL(v) ? v : undefined }
function storedURL(v: unknown) { return v === null || publicSubscriptionDraftURL(v) ? v : undefined }
// This DTO intentionally permits null only for a URL retained by original
// entity/revision. It is never passed directly to a write or preview API.
export interface TemplateRecoveryCustomization {
  version: 3; mode: string; mixed_enabled: boolean; mixed_port: number; system_proxy: boolean
  tun: SubscriptionTemplateCustomization['tun']; dns: Omit<SubscriptionTemplateCustomization['dns'], 'servers'> & { servers: Record<string, any>[] }
  main_group: string; policy_groups: Record<string, any>[]; final: string; rule_sets: Record<string, any>[]; advanced_source: ''
}
export function templateRecoveryCustomization(v: unknown, original?: SubscriptionTemplateCustomization, capture = false): TemplateRecoveryCustomization | undefined {
  if (!record(v) || !keys(v, ['version', 'mode', 'mixed_enabled', 'mixed_port', 'system_proxy', 'tun', 'dns', 'main_group', 'policy_groups', 'final', 'rule_sets', 'advanced_source']) || v.version !== 3 || !['rule', 'global', 'direct'].includes(v.mode) || typeof v.mixed_enabled !== 'boolean' || !int(v.mixed_port) || typeof v.system_proxy !== 'boolean' || !text(v.main_group, 256) || !text(v.final, 256) || (v.advanced_source !== undefined && v.advanced_source !== '') || !record(v.tun) || !record(v.dns)) return
  const tun = v.tun, dns = v.dns
  if (!keys(tun, ['enabled', 'addresses', 'mtu', 'auto_route', 'strict_route', 'dns_hijack']) || !texts(tun.addresses, 256, 16) || !int(tun.mtu) || ['enabled', 'auto_route', 'strict_route', 'dns_hijack'].some(key => typeof tun[key] !== 'boolean')) return
  if (!keys(dns, ['enabled', 'servers', 'default_server', 'strategy', 'cache_enabled', 'cache_capacity', 'fake_ip_enabled', 'fake_ipv4_range', 'fake_ipv6_range']) || ['enabled', 'cache_enabled', 'fake_ip_enabled'].some(key => typeof dns[key] !== 'boolean') || !text(dns.default_server, 256) || !['prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only'].includes(dns.strategy) || !int(dns.cache_capacity) || !text(dns.fake_ipv4_range, 256) || (dns.fake_ipv6_range !== undefined && !text(dns.fake_ipv6_range, 256)) || !Array.isArray(dns.servers) || dns.servers.length > 32 || !Array.isArray(v.policy_groups) || v.policy_groups.length > 64 || !Array.isArray(v.rule_sets) || v.rule_sets.length > 256) return
  const servers: Record<string, any>[] = []
  for (const server of dns.servers) {
    if (!record(server) || !keys(server, ['tag', 'type', 'host', 'port', 'path', 'server_name']) || !text(server.tag, 256) || !['system', 'udp', 'tcp', 'doh', 'dot', 'doq'].includes(server.type) || (server.host !== undefined && (!text(server.host, 256) || /[^0-9a-fA-F:.]/.test(server.host))) || (server.port !== undefined && !int(server.port)) || (server.server_name !== undefined && (!text(server.server_name, 512) || /[^a-zA-Z0-9.-]/.test(server.server_name)))) return
    const next: Record<string, any> = { tag: server.tag, type: server.type }
    for (const key of ['host', 'port', 'server_name']) if (server[key] !== undefined) next[key] = server[key]
    if (server.path !== undefined) {
      const origin = original?.dns.servers.find(item => item.tag === server.tag)?.path
      // Only standard non-authenticated DoH paths are stored. Other unchanged
      // server paths remain a revision-bound reference and are freshly re-read.
      const path = capture && server.path === origin ? null : server.path
      if (path !== null && path !== '' && path !== '/' && path !== '/dns-query') return
      next.path = path
    }
    servers.push(next)
  }
  const groups: Record<string, any>[] = []
  for (const group of v.policy_groups) {
    if (!record(group) || !keys(group, ['id', 'name', 'type', 'include_pattern', 'exclude_pattern', 'include_groups', 'default_group', 'probe_url', 'interval', 'tolerance', 'include_direct', 'include_reject']) || !text(group.id, 256) || !text(group.name, 256) || !['select', 'urltest', 'fallback'].includes(group.type)) return
    const next: Record<string, any> = { id: group.id, name: group.name, type: group.type }
    for (const key of ['include_pattern', 'exclude_pattern', 'default_group']) if (group[key] !== undefined) { if (!text(group[key], 2048)) return; next[key] = group[key] }
    for (const key of ['interval', 'tolerance']) if (group[key] !== undefined) { if (!int(group[key])) return; next[key] = group[key] }
    for (const key of ['include_direct', 'include_reject']) if (group[key] !== undefined) { if (typeof group[key] !== 'boolean') return; next[key] = group[key] }
    if (group.include_groups !== undefined) { if (!texts(group.include_groups, 256, 64)) return; next.include_groups = [...group.include_groups] }
    if (group.probe_url !== undefined) {
      const url = capture ? capturedURL(group.probe_url, original?.policy_groups.find(item => item.id === group.id)?.probe_url) : storedURL(group.probe_url)
      if (url === undefined) return
      next.probe_url = url
    }
    groups.push(next)
  }
  const rules: Record<string, any>[] = []
  for (const rule of v.rule_sets) {
    if (!record(rule) || !keys(rule, ['rule_set_id', 'tag', 'url', 'behavior', 'format', 'target', 'interval']) || !text(rule.target, 256)) return
    const next: Record<string, any> = { target: rule.target }
    if (rule.rule_set_id !== undefined) { if (!int(rule.rule_set_id) || rule.rule_set_id <= 0) return; next.rule_set_id = rule.rule_set_id }
    if (rule.tag !== undefined) { if (!text(rule.tag, 256)) return; next.tag = rule.tag }
    if (rule.behavior !== undefined) { if (!['domain', 'ipcidr', 'classical'].includes(rule.behavior)) return; next.behavior = rule.behavior }
    if (rule.format !== undefined) { if (!['domain_list', 'cidr_list', 'zero_rule_ir', 'zrs', 'yaml', 'text', 'mrs', 'source', 'binary'].includes(rule.format)) return; next.format = rule.format }
    if (rule.interval !== undefined) { if (!int(rule.interval)) return; next.interval = rule.interval }
    if (rule.url !== undefined) {
      const url = capture ? capturedURL(rule.url, original?.rule_sets.find(item => item.tag === rule.tag && !item.rule_set_id)?.url) : storedURL(rule.url)
      if (url === undefined) return
      next.url = url
    }
    rules.push(next)
  }
  return { version: 3, mode: v.mode, mixed_enabled: v.mixed_enabled, mixed_port: v.mixed_port, system_proxy: v.system_proxy,
    tun: { enabled: tun.enabled, addresses: [...tun.addresses], mtu: tun.mtu, auto_route: tun.auto_route, strict_route: tun.strict_route, dns_hijack: tun.dns_hijack },
    dns: { enabled: dns.enabled, servers, default_server: dns.default_server, strategy: dns.strategy, cache_enabled: dns.cache_enabled, cache_capacity: dns.cache_capacity, fake_ip_enabled: dns.fake_ip_enabled, fake_ipv4_range: dns.fake_ipv4_range, ...(dns.fake_ipv6_range !== undefined ? { fake_ipv6_range: dns.fake_ipv6_range } : {}) },
    main_group: v.main_group, policy_groups: groups, final: v.final, rule_sets: rules, advanced_source: '' }
}
export interface TemplateRecoveryFields { name: string; slug: string; description: string; renderer: string; customization: TemplateRecoveryCustomization; is_active: boolean; sort_order: number }
export function templateRecoveryFields(v: unknown, original?: SubscriptionTemplateCustomization, capture = false): TemplateRecoveryFields | undefined {
  if (!record(v) || !text(v.name, 320) || !text(v.slug, 320) || !text(v.description, 1020) || !renderers.includes(v.renderer) || typeof v.is_active !== 'boolean' || !int(v.sort_order)) return
  const customization = templateRecoveryCustomization(v.customization, original, capture)
  if (!customization) return
  return { name: v.name, slug: v.slug, description: v.description, renderer: v.renderer, customization, is_active: v.is_active, sort_order: v.sort_order }
}
export interface SubscriptionTemplateRecoveryDraft { id: number; revision: number; fields: TemplateRecoveryFields; baseline: TemplateRecoveryFields | null; rendererDrafts: { renderer: string; customization: TemplateRecoveryCustomization }[] }
export function subscriptionTemplateRecoveryDraft(v: unknown): SubscriptionTemplateRecoveryDraft | undefined {
  if (!record(v) || !int(v.id) || v.id < 0 || !int(v.revision) || v.revision < 0 || !Array.isArray(v.rendererDrafts) || v.rendererDrafts.length > renderers.length) return
  const fields = templateRecoveryFields(v.fields), baseline = v.baseline === null ? null : templateRecoveryFields(v.baseline)
  if (!fields || baseline === undefined || (v.id > 0 && !baseline) || (v.id === 0 && (v.revision !== 0 || baseline !== null))) return
  const rendererDrafts: SubscriptionTemplateRecoveryDraft['rendererDrafts'] = []
  for (const entry of v.rendererDrafts) {
    if (!record(entry) || !renderers.includes(entry.renderer) || rendererDrafts.some(item => item.renderer === entry.renderer)) return
    const customization = templateRecoveryCustomization(entry.customization)
    if (!customization) return
    rendererDrafts.push({ renderer: entry.renderer, customization })
  }
  return { id: v.id, revision: v.revision, fields, baseline, rendererDrafts }
}
export function restoreTemplateCustomization(v: TemplateRecoveryCustomization, original: SubscriptionTemplateCustomization): SubscriptionTemplateCustomization {
  // URL references never become writes as null or empty credentials. Missing
  // matches are only display placeholders; the caller must block submission.
  return { ...v, dns: { ...v.dns, servers: v.dns.servers.map(server => ({ ...server, ...(server.path === null ? { path: original.dns.servers.find(item => item.tag === server.tag)?.path || '' } : {}) })) },
    policy_groups: v.policy_groups.map(group => ({ ...group, ...(group.probe_url === null ? { probe_url: original.policy_groups.find(item => item.id === group.id)?.probe_url || '' } : {}) })),
    rule_sets: v.rule_sets.map(rule => ({ ...rule, ...(rule.url === null ? { url: original.rule_sets.find(item => item.tag === rule.tag && !item.rule_set_id)?.url || '' } : {}) })) } as SubscriptionTemplateCustomization
}
export interface RuleSetRecoveryFields { name: string; description: string; tag: string; mode: 'manual' | 'remote'; source_url: string | null; source_format: string; content: string; sync_interval: number; is_active: boolean }
export function ruleSetRecoveryFields(v: unknown, originalURL?: string, capture = false): RuleSetRecoveryFields | undefined {
  if (!record(v) || !text(v.name, 320) || !text(v.description, 1020) || !text(v.tag, 256) || !['manual', 'remote'].includes(v.mode) || !['auto', 'zero_rule_ir', 'domain_list', 'cidr_list', 'clash_classical'].includes(v.source_format) || !text(v.content, 4 * 1024 * 1024) || !int(v.sync_interval) || typeof v.is_active !== 'boolean') return
  const url = capture ? capturedURL(v.source_url, originalURL) : storedURL(v.source_url)
  if (url === undefined) return
  return { name: v.name, description: v.description, tag: v.tag, mode: v.mode, source_url: url, source_format: v.source_format, content: v.content, sync_interval: v.sync_interval, is_active: v.is_active }
}
export interface RuleSetRecoveryDraft { id: number; revision: number; fields: RuleSetRecoveryFields; baseline: RuleSetRecoveryFields | null }
export function ruleSetRecoveryDraft(v: unknown): RuleSetRecoveryDraft | undefined {
  if (!record(v) || !int(v.id) || v.id < 0 || !int(v.revision) || v.revision < 0) return
  const fields = ruleSetRecoveryFields(v.fields), baseline = v.baseline === null ? null : ruleSetRecoveryFields(v.baseline)
  if (!fields || baseline === undefined || (v.id > 0 && !baseline) || (v.id === 0 && (v.revision !== 0 || baseline !== null || fields.source_url === null))) return
  return { id: v.id, revision: v.revision, fields, baseline }
}
// Retain reference identity even while a deleted/moved server resource cannot be
// re-read. Only null-reference placeholders are added; edited public URL strings
// never become server references merely because a second recovery occurred.
export function templateURLReferenceOrigin(v: TemplateRecoveryCustomization, original: SubscriptionTemplateCustomization): SubscriptionTemplateCustomization {
  const value: SubscriptionTemplateCustomization = {
    ...original, dns: { ...original.dns, servers: original.dns.servers.map(server => ({ ...server })) },
    policy_groups: original.policy_groups.map(group => ({ ...group })), rule_sets: original.rule_sets.map(rule => ({ ...rule })),
  }
  for (const group of v.policy_groups) if (group.probe_url === null && !value.policy_groups.some(item => item.id === group.id)) value.policy_groups.push({ id: group.id, name: '', type: 'select', probe_url: '' })
  for (const rule of v.rule_sets) if (rule.url === null && !value.rule_sets.some(item => item.tag === rule.tag && !item.rule_set_id)) value.rule_sets.push({ tag: rule.tag, target: 'direct', url: '' })
  for (const server of v.dns.servers) if (server.path === null && !value.dns.servers.some(item => item.tag === server.tag)) value.dns.servers.push({ tag: server.tag, type: 'doh', path: '' })
  return value
}
