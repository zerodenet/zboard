// Explicit field allowlists exclude structured token/password/private-key,
// quote/funding/payment-authorization and attachment fields. Ordinary support
// textarea content is intentionally preserved verbatim, not scanned or redacted.
type Scalar = string | number | boolean
export const planDraftFields = ['name', 'slug', 'summary', 'description', 'node_group_id', 'traffic_bytes', 'speed_limit_mbps', 'max_active_subscriptions', 'is_renewable', 'device_limit', 'family_limit', 'reset_policy', 'traffic_calc_mode', 'is_active', 'sort_order'] as const
const textLimits: Record<string, number> = { name: 80, slug: 80, summary: 255, description: 20_000 }
const booleans = new Set(['is_renewable', 'is_active'])
export function pickPlanDraft(value: any): Record<string, Scalar> | undefined {
  if (!value || typeof value !== 'object') return
  const out: Record<string, Scalar> = {}
  for (const key of planDraftFields) {
    if (textLimits[key]) {
      if (typeof value[key] !== 'string' || new TextEncoder().encode(value[key]).byteLength > textLimits[key]) return
    } else if (booleans.has(key)) { if (typeof value[key] !== 'boolean') return }
    else if (!Number.isSafeInteger(value[key])) return
    out[key] = value[key]
  }
  return out
}
export interface PlanRecoveryDraft { entityID: number; revision: number; baseline: string; syncTraffic: boolean; fields: Record<string, Scalar> }
export function planRecoveryDraft(value: any): PlanRecoveryDraft | undefined {
  const fields = pickPlanDraft(value?.fields)
  if (!fields || !Number.isSafeInteger(value.entityID) || value.entityID < 0 || !Number.isSafeInteger(value.revision) || value.revision < 0 || typeof value.baseline !== 'string' || value.baseline.length > 32_000) return
  if (value.syncTraffic !== undefined && typeof value.syncTraffic !== 'boolean') return
  return { entityID: value.entityID, revision: value.revision, baseline: value.baseline, syncTraffic: value.syncTraffic === true, fields }
}
const skuKeys = ['code', 'name', 'sku_type', 'billing_mode', 'entitlement_mode', 'renewal_effect', 'billing_unit', 'billing_value', 'price_cents', 'currency', 'grant_traffic_bytes', 'is_active', 'sort_order'] as const
export function pickSKUDraft(value: any): Record<string, Scalar | string[]> | undefined {
  if (!value || typeof value !== 'object') return
  const out: Record<string, Scalar | string[]> = {}
  for (const key of skuKeys) {
    const field = value[key]
    if (key === 'is_active') { if (typeof field !== 'boolean') return }
    else if (['billing_value', 'price_cents', 'grant_traffic_bytes', 'sort_order'].includes(key)) { if (!Number.isSafeInteger(field)) return }
    else if (typeof field !== 'string' || field.length > 160) return
    out[key] = field
  }
  if (!Array.isArray(value.allowed_operations) || value.allowed_operations.length > 5 || value.allowed_operations.some((v: unknown) => !['purchase', 'renew', 'change', 'addon', 'reset'].includes(String(v)))) return
  out.allowed_operations = [...value.allowed_operations]
  return out
}
export function createPlanRecoveryDraft(value: any): (PlanRecoveryDraft & { sku: Record<string, Scalar | string[]> }) | undefined {
  const plan = planRecoveryDraft(value), sku = pickSKUDraft(value?.sku)
  if (plan?.entityID !== 0 || !sku) return
  return { ...plan, sku }
}
export function skuRecoveryDraft(value: any): { entityID: number; planID: number; baseline: string; fields: Record<string, Scalar | string[]> } | undefined {
  const fields = pickSKUDraft(value?.fields)
  if (!fields || !Number.isSafeInteger(value.entityID) || value.entityID < 0 || !Number.isSafeInteger(value.planID) || value.planID <= 0 || typeof value.baseline !== 'string' || value.baseline.length > 8192) return
  return { entityID: value.entityID, planID: value.planID, baseline: value.baseline, fields }
}
function text(value: unknown, max: number): value is string { return typeof value === 'string' && Array.from(value).length <= max }
export interface TicketRecoveryDraft { subject: string; category: 'connection' | 'billing' | 'account' | 'other'; priority: 1 | 2; body: string; references: { order_id: string; subscription_id: string; wallet_entry_id: string } }
export function ticketRecoveryDraft(value: any): TicketRecoveryDraft | undefined {
  if (!text(value?.subject, 160) || !text(value.body, 5000) || !['connection', 'billing', 'account', 'other'].includes(value.category) || ![1, 2].includes(value.priority)) return
  const refs = value.references
  if (!refs || !text(refs.order_id, 64) || !text(refs.subscription_id, 64) || !text(refs.wallet_entry_id, 64)) return
  return { subject: value.subject, category: value.category, priority: value.priority, body: value.body, references: { order_id: refs.order_id, subscription_id: refs.subscription_id, wallet_entry_id: refs.wallet_entry_id } }
}
export function ticketReplyRecoveryDraft(value: any): { ticketID: number; body: string } | undefined {
  if (!Number.isSafeInteger(value?.ticketID) || value.ticketID <= 0 || !text(value.body, 5000)) return
  return { ticketID: value.ticketID, body: value.body }
}
