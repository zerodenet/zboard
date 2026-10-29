import type { SystemConfig } from '../api/client'

export const maintenanceRecoveryKeys = ['maintenance_title', 'maintenance_message', 'maintenance_enabled'] as const
export interface MaintenanceRecoveryBaseline {
  maintenance_title: { value: string; revision: number }
  maintenance_message: { value: string; revision: number }
  // The original server value is a CAS boundary, never the edited switch.
  maintenance_enabled: { value: boolean; revision: number }
}
export interface MaintenanceRecoveryDraft {
  title: string
  message: string
  baseline: MaintenanceRecoveryBaseline
}
const record = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value)
const exactKeys = (value: Record<string, unknown>, keys: readonly string[]) => Object.keys(value).length === keys.length && Object.keys(value).every(key => keys.includes(key))
const text = (value: unknown, limit: number): value is string => typeof value === 'string' && new TextEncoder().encode(value).byteLength <= limit
const revision = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) >= 0

function baseline(value: unknown): MaintenanceRecoveryBaseline | undefined {
  if (!record(value) || !exactKeys(value, maintenanceRecoveryKeys)) return
  const title = value.maintenance_title, message = value.maintenance_message, enabled = value.maintenance_enabled
  if (!record(title) || !record(message) || !record(enabled) || ![title, message, enabled].every(item => exactKeys(item, ['value', 'revision']) && revision(item.revision)) || !text(title.value, 1024) || !text(message.value, 32000) || typeof enabled.value !== 'boolean') return
  return {
    maintenance_title: { value: title.value, revision: title.revision as number },
    maintenance_message: { value: message.value, revision: message.revision as number },
    maintenance_enabled: { value: enabled.value, revision: enabled.revision as number },
  }
}
export function maintenanceConfigBaseline(configs: SystemConfig[]): MaintenanceRecoveryBaseline | undefined {
  const rows = maintenanceRecoveryKeys.map(key => configs.filter(item => item.config_key === key))
  if (rows.some(items => items.length !== 1)) return
  const title = rows[0]![0]!, message = rows[1]![0]!, enabled = rows[2]![0]!
  if ([title, message, enabled].some(item => item.is_secret) || title.value_type !== 'string' || message.value_type !== 'string' || enabled.value_type !== 'bool') return
  return baseline({
    maintenance_title: { value: title.value, revision: title.revision },
    maintenance_message: { value: message.value, revision: message.revision },
    maintenance_enabled: { value: enabled.value, revision: enabled.revision },
  })
}
export function maintenanceRecoveryDraft(value: unknown): MaintenanceRecoveryDraft | undefined {
  // Unknown execution/DSN/preflight fields reject the entire payload. Nothing
  // is truncated or silently serialized from the whole MaintenanceState.
  if (!record(value) || !exactKeys(value, ['title', 'message', 'baseline']) || !text(value.title, 1024) || !text(value.message, 32000)) return
  const original = baseline(value.baseline)
  if (!original) return
  return { title: value.title, message: value.message, baseline: original }
}
export function sameMaintenanceBaseline(left: MaintenanceRecoveryBaseline, right: MaintenanceRecoveryBaseline) {
  return maintenanceRecoveryKeys.every(key => left[key].revision === right[key].revision && left[key].value === right[key].value)
}
