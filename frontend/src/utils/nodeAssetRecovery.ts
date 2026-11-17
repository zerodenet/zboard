// Ordinary asset fields only. Never serialize SSH, credentials, tasks or commands.
export interface NodeAssetFields { name: string; region: string; address: string; remark: string; lifecycle_status: string; is_enabled: boolean }
export function pickNodeAssetFields(value: any): NodeAssetFields | undefined {
  if (!value || typeof value !== 'object') return
  const limits = { name: 1024, region: 1024, address: 4096, remark: 20_000 }
  const fields: Record<string, string> = {}
  for (const [key, limit] of Object.entries(limits)) {
    if (typeof value[key] !== 'string' || new TextEncoder().encode(value[key]).byteLength > limit) return
    fields[key] = value[key]
  }
  if (!['active', 'maintenance', 'retired', 'deleting'].includes(value.lifecycle_status) || typeof value.is_enabled !== 'boolean') return
  return { name: fields.name!, region: fields.region!, address: fields.address!, remark: fields.remark!, lifecycle_status: value.lifecycle_status, is_enabled: value.is_enabled }
}
export function nodeAssetBaseline(value: any): string {
  const fields = pickNodeAssetFields({ ...value, region: value?.region || '', address: value?.address || '', remark: value?.remark || '', lifecycle_status: value?.lifecycle_status || 'active', is_enabled: Boolean(value?.is_enabled) })
  return fields ? JSON.stringify(fields) : ''
}
export interface NodeAssetRecovery { entityID: number; baseline: string; fields: NodeAssetFields }
export function nodeAssetRecovery(value: any): NodeAssetRecovery | undefined {
  const fields = pickNodeAssetFields(value?.fields)
  if (!fields || !Number.isSafeInteger(value?.entityID) || value.entityID < 0 || typeof value.baseline !== 'string' || value.baseline.length > 32_000) return
  if (value.entityID > 0 && !value.baseline) return
  return { entityID: value.entityID, baseline: value.baseline, fields }
}
