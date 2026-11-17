import type { AdminAnnouncement } from '../api/client'
import type { Surface } from '../api/plugins'

// Ordinary editor content only. These allowlists deliberately omit publication,
// approval, execution, delivery, auto_run, credentials and financial authority.
const record = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown, max: number): value is string => typeof value === 'string' && new TextEncoder().encode(value).byteLength <= max
const revision = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) >= 0
const surface = (value: unknown): value is Surface => value === 'admin' || value === 'account' || value === 'public'
export interface AnnouncementDraftFields {
  title: string; content: string; severity: AdminAnnouncement['severity']; audience: AdminAnnouncement['audience']
  popup_enabled: boolean; dismissible: boolean; starts_at: string; ends_at: string
}
export function announcementDraftFields(value: unknown): AnnouncementDraftFields | undefined {
  if (!record(value) || !text(value.title, 640) || !text(value.content, 65536) || !['info', 'success', 'warning', 'critical'].includes(String(value.severity)) || !['all', 'guest', 'user', 'admin'].includes(String(value.audience)) || typeof value.popup_enabled !== 'boolean' || typeof value.dismissible !== 'boolean' || !text(value.starts_at, 64) || !text(value.ends_at, 64)) return
  return { title: value.title, content: value.content, severity: value.severity as AnnouncementDraftFields['severity'], audience: value.audience as AnnouncementDraftFields['audience'], popup_enabled: value.popup_enabled, dismissible: value.dismissible, starts_at: value.starts_at, ends_at: value.ends_at }
}
export interface AnnouncementRecoveryDraft { id: number; revision: number; fields: AnnouncementDraftFields; baseline: AnnouncementDraftFields | null }
export function announcementRecoveryDraft(value: unknown): AnnouncementRecoveryDraft | undefined {
  if (!record(value) || !revision(value.id) || !revision(value.revision)) return
  const fields = announcementDraftFields(value.fields), baseline = value.baseline === null ? null : announcementDraftFields(value.baseline)
  if (!fields || baseline === undefined || (value.id > 0 && !baseline) || (value.id === 0 && (value.revision !== 0 || baseline !== null))) return
  return { id: value.id, revision: value.revision, fields, baseline }
}
export interface MenuDraftNode { id: string; parent_id: string; label: string; icon: string; path: string; position: number; hidden: boolean }
export interface MenuDraftTree { revision: number; nodes: MenuDraftNode[] }
export function menuDraftTree(value: unknown): MenuDraftTree | undefined {
  if (!record(value) || !revision(value.revision) || !Array.isArray(value.nodes) || value.nodes.length > 5000) return
  const nodes: MenuDraftNode[] = [], ids = new Set<string>()
  for (const node of value.nodes) {
    if (!record(node) || !text(node.id, 512) || !node.id || ids.has(node.id) || !text(node.parent_id, 512) || !text(node.label, 640) || !text(node.icon, 256) || !text(node.path, 8192) || !Number.isSafeInteger(node.position) || typeof node.hidden !== 'boolean') return
    ids.add(node.id)
    nodes.push({ id: node.id, parent_id: node.parent_id, label: node.label, icon: node.icon, path: node.path, position: Number(node.position), hidden: node.hidden })
  }
  return { revision: value.revision, nodes }
}
export interface MenuRecoveryDraft { surface: Surface; tree: MenuDraftTree; baseline: MenuDraftTree }
export function menuRecoveryDraft(value: unknown): MenuRecoveryDraft | undefined {
  if (!record(value) || !surface(value.surface)) return
  const tree = menuDraftTree(value.tree), baseline = menuDraftTree(value.baseline)
  if (!tree || !baseline || tree.revision !== baseline.revision) return
  return { surface: value.surface, tree, baseline }
}
export interface TaskRecoveryDraft {
  type: 'quota' | 'email'; max_attempts: number; scope_mode: 'all' | 'users' | 'subscriptions'; scope_ids: number[]
  quota: { delta_mb: number; reason: string } | null; email: { subject: string; body: string } | null
}
export function taskRecoveryDraft(value: unknown): TaskRecoveryDraft | undefined {
  if (!record(value) || (value.type !== 'quota' && value.type !== 'email') || !Number.isSafeInteger(value.max_attempts) || Number(value.max_attempts) < 1 || Number(value.max_attempts) > 10 || !['all', 'users', 'subscriptions'].includes(String(value.scope_mode)) || (value.type === 'email' && value.scope_mode === 'subscriptions') || !Array.isArray(value.scope_ids) || value.scope_ids.length > 5000 || value.scope_ids.some(id => !Number.isSafeInteger(id) || id <= 0) || new Set(value.scope_ids).size !== value.scope_ids.length || (value.scope_mode === 'all' && value.scope_ids.length !== 0)) return
  let quota: TaskRecoveryDraft['quota'] = null, email: TaskRecoveryDraft['email'] = null
  if (value.type === 'quota') {
    if (!record(value.quota) || !Number.isSafeInteger(value.quota.delta_mb) || Math.abs(Number(value.quota.delta_mb)) > 1_000_000_000 || !text(value.quota.reason, 1020)) return
    quota = { delta_mb: Number(value.quota.delta_mb), reason: value.quota.reason }
  } else {
    if (!record(value.email) || !text(value.email.subject, 800) || !text(value.email.body, 400000)) return
    email = { subject: value.email.subject, body: value.email.body }
  }
  return { type: value.type, max_attempts: Number(value.max_attempts), scope_mode: value.scope_mode as TaskRecoveryDraft['scope_mode'], scope_ids: [...value.scope_ids] as number[], quota, email }
}
