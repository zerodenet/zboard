// Explicit, same-tab recovery only. Payloads are never read before fresh
// identity AND page permission checks. Nothing here dispatches a business write.
export const REAUTH_DRAFT_TTL_MS = 10 * 60_000
export const REAUTH_DRAFT_MAX_BYTES = 64 * 1024
export const REAUTH_DRAFT_TOTAL_BYTES = 192 * 1024
export const REAUTH_DRAFT_MANIFEST = 'zboard.reauth-drafts.manifest.v1'
const prefix = 'zboard.reauth-draft.v1.'
const maxEntries = 4
type Permission = 'account' | 'admin'
interface Entry { key: string; scope: string; route: string; permission: Permission; bytes: number }
interface Manifest { version: 1; actorID: number; createdAt: number; expiresAt: number; route: string; entries: Entry[] }
export interface DraftRegistration<T> {
  actorID: number
  scope: string
  permission: Permission
  route: () => string
  read: () => unknown | null
  sanitize: (value: unknown) => T | undefined
}
const registrations = new Set<DraftRegistration<unknown>>()
let verifiedActor = 0
let verifiedAdmin = false
const verifiedPages = new Set<string>()
const bytes = (value: string) => new TextEncoder().encode(value).byteLength
const validID = (value: number) => Number.isSafeInteger(value) && value > 0
const queryKeys = new Set(['q', 'page', 'limit', 'plan', 'sku', 'editor', 'sku_q', 'sku_active', 'sku_page', 'sku_limit', 'active', 'ticket', 'new', 'order_id', 'subscription_id', 'wallet_entry_id', 'status', 'category', 'tab', 'view', 'kind', 'node', 'endpoint', 'group', 'subscription', 'operation'])

// Page position and business selections only, never OAuth/checkout credentials,
// quote fingerprints, payment authorizations or an opaque return_to URL.
export function safeReauthenticationRoute(value: string) {
  if (!value.startsWith('/') || value.startsWith('//') || value.length > 2048) return ''
  try {
    const url = new URL(value, 'https://draft.invalid')
    if (url.origin !== 'https://draft.invalid' || !/^\/(admin|account)(\/|$)/.test(url.pathname)) return ''
    for (const key of [...url.searchParams.keys()]) if (!queryKeys.has(key)) url.searchParams.delete(key)
    if (!/^#[a-zA-Z0-9._-]{0,80}$/.test(url.hash)) url.hash = ''
    return url.pathname + url.search + url.hash
  } catch { return '' }
}
function routeKey(value: string) {
  const safe = safeReauthenticationRoute(value)
  if (!safe) return ''
  const url = new URL(safe, 'https://draft.invalid')
  url.searchParams.sort()
  return url.pathname + url.search + url.hash
}
function pagePath(value: string) { return value.split(/[?#]/, 1)[0] }
function readManifest(): Manifest | null {
  try {
    const raw = sessionStorage.getItem(REAUTH_DRAFT_MANIFEST)
    if (!raw || bytes(raw) > 8192) return null
    const m = JSON.parse(raw) as Manifest
    const now = Date.now()
    if (m?.version !== 1 || !validID(m.actorID) || !Number.isFinite(m.createdAt) || !Number.isFinite(m.expiresAt)
      || m.createdAt > now || m.expiresAt - m.createdAt !== REAUTH_DRAFT_TTL_MS || m.expiresAt <= now
      || routeKey(m.route) !== m.route || !Array.isArray(m.entries) || m.entries.length > maxEntries) return null
    if (m.entries.some(e => !e || typeof e.scope !== 'string' || !/^[a-z][a-z0-9.-]{1,63}$/.test(e.scope)
      || e.key !== prefix + e.scope || e.route !== m.route || !['admin', 'account'].includes(e.permission)
      || !Number.isSafeInteger(e.bytes) || e.bytes < 0 || e.bytes > REAUTH_DRAFT_MAX_BYTES)) return null
    if (new Set(m.entries.map(e => e.key)).size !== m.entries.length || m.entries.reduce((n, e) => n + e.bytes, bytes(raw)) > REAUTH_DRAFT_TOTAL_BYTES) return null
    return m
  } catch { return null }
}
export function invalidateReauthenticationDraftIdentity() { verifiedActor = 0; verifiedAdmin = false; verifiedPages.clear() }
export function clearReauthenticationDrafts() {
  invalidateReauthenticationDraftIdentity()
  try {
    const keys: string[] = []
    for (let i = 0; i < sessionStorage.length; i++) { const key = sessionStorage.key(i); if (key?.startsWith(prefix)) keys.push(key) }
    for (const key of keys) sessionStorage.removeItem(key)
    sessionStorage.removeItem(REAUTH_DRAFT_MANIFEST)
  } catch { /* No optional storage may prevent sign-out. */ }
}
export function registerReauthenticationDraft<T>(registration: DraftRegistration<T>) {
  registrations.add(registration as DraftRegistration<unknown>)
  return () => registrations.delete(registration as DraftRegistration<unknown>)
}
export function captureReauthenticationDrafts(actorID: number, path: string): { ok: boolean; count: number; message: string } {
  clearReauthenticationDrafts()
  const route = routeKey(path)
  if (!validID(actorID) || !route) return { ok: false, count: 0, message: '无法确认当前页面，未离开此页。' }
  const now = Date.now()
  const manifest: Manifest = { version: 1, actorID, createdAt: now, expiresAt: now + REAUTH_DRAFT_TTL_MS, route, entries: [] }
  const payloads: [string, string][] = []
  try {
    for (const item of registrations) {
      if (item.actorID !== actorID || routeKey(item.route()) !== route) continue
      const value = item.read()
      if (value == null) continue
      const data = item.sanitize(value)
      if (!data || !/^[a-z][a-z0-9.-]{1,63}$/.test(item.scope)) throw new Error('invalid draft')
      const key = prefix + item.scope
      const raw = JSON.stringify({ version: 1, actorID, route, scope: item.scope, createdAt: now, expiresAt: manifest.expiresAt, data })
      const size = bytes(key) + bytes(raw)
      if (size > REAUTH_DRAFT_MAX_BYTES || payloads.length >= maxEntries || manifest.entries.some(e => e.scope === item.scope)) throw new Error('draft limit')
      payloads.push([key, raw]); manifest.entries.push({ key, scope: item.scope, route, permission: item.permission, bytes: size })
    }
    if (!payloads.length) return { ok: true, count: 0, message: '' }
    const meta = JSON.stringify(manifest)
    if (bytes(meta) > 8192 || manifest.entries.reduce((n, e) => n + e.bytes, bytes(meta)) > REAUTH_DRAFT_TOTAL_BYTES) throw new Error('total limit')
    for (const [key, raw] of payloads) sessionStorage.setItem(key, raw)
    sessionStorage.setItem(REAUTH_DRAFT_MANIFEST, meta)
    return { ok: true, count: payloads.length, message: '' }
  } catch {
    clearReauthenticationDrafts()
    return { ok: false, count: 0, message: '当前草稿过大或浏览器无法暂存，尚未离开此页。请缩短内容或自行另存后重试；也可继续使用本页密码认证。' }
  }
}
// Call only after /auth/me returned for the exact token being installed.
// This reads metadata only, never a draft payload.
export function confirmReauthenticationDraftIdentity(actorID: number, isAdmin: boolean) {
  invalidateReauthenticationDraftIdentity()
  const manifest = readManifest()
  if (!validID(actorID) || !manifest || manifest.actorID !== actorID) { clearReauthenticationDrafts(); verifiedActor = actorID; verifiedAdmin = isAdmin; return }
  verifiedActor = actorID; verifiedAdmin = isAdmin
  if (!isAdmin && manifest.entries.some(e => e.permission === 'admin')) clearReauthenticationDrafts()
}
export function hasReauthenticationDraft(actorID: number, scope: string, route: string, permission: Permission) {
  if (verifiedActor !== actorID || !validID(actorID) || permission === 'admin' && !verifiedAdmin) return false
  const m = readManifest()
  if (!m) { clearReauthenticationDrafts(); return false }
  return m.actorID === actorID && m.route === routeKey(route) && m.entries.some(e => e.scope === scope && e.permission === permission)
}
export function pendingReauthenticationDraftPage(actorID: number) {
  if (verifiedActor !== actorID || !validID(actorID)) return ''
  const m = readManifest()
  return m?.actorID === actorID ? pagePath(m.route) : ''
}
export function approveReauthenticationDraftPage(actorID: number, route: string, allowed: boolean) {
  if (verifiedActor !== actorID) return
  if (!allowed) { clearReauthenticationDrafts(); return }
  verifiedPages.add(pagePath(route))
}
export function takeReauthenticationDraft<T>(actorID: number, scope: string, route: string, permission: Permission, sanitize: (value: unknown) => T | undefined): T | undefined {
  if (!verifiedPages.has(pagePath(route)) || !hasReauthenticationDraft(actorID, scope, route, permission)) return
  const m = readManifest()
  if (!m) { clearReauthenticationDrafts(); return }
  const entry = m.entries.find(e => e.scope === scope)!
  try {
    const raw = sessionStorage.getItem(entry.key)
    if (!raw || bytes(raw) + bytes(entry.key) !== entry.bytes || bytes(raw) > REAUTH_DRAFT_MAX_BYTES) throw new Error('invalid draft size')
    const p = JSON.parse(raw)
    if (p?.version !== 1 || p.actorID !== actorID || p.scope !== scope || p.route !== m.route || p.createdAt !== m.createdAt || p.expiresAt !== m.expiresAt) throw new Error('invalid draft binding')
    const data = sanitize(p.data)
    sessionStorage.removeItem(entry.key)
    m.entries = m.entries.filter(e => e.scope !== scope)
    if (m.entries.length) sessionStorage.setItem(REAUTH_DRAFT_MANIFEST, JSON.stringify(m))
    else sessionStorage.removeItem(REAUTH_DRAFT_MANIFEST)
    return data
  } catch { clearReauthenticationDrafts(); return }
}
