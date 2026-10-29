// Membership IDs are business selections, never endpoint configuration or secrets.
export interface NodeGroupFields { name: string; code: string; description: string; is_enabled: boolean; protocol_endpoint_ids: number[]; network_entry_ids: number[] }
function ids(value: unknown): number[] | undefined {
  if (!Array.isArray(value) || value.length > 10000 || value.some(id => !Number.isSafeInteger(id) || id <= 0) || new Set(value).size !== value.length) return
  return [...value]
}
export function pickNodeGroupFields(value: any): NodeGroupFields | undefined {
  if (!value || typeof value !== 'object') return
  for (const [key, max] of [['name', 1024], ['code', 1024], ['description', 4096]] as const) {
    if (typeof value[key] !== 'string' || new TextEncoder().encode(value[key]).byteLength > max) return
  }
  const endpoints = ids(value.protocol_endpoint_ids), entries = ids(value.network_entry_ids)
  if (!endpoints || !entries || typeof value.is_enabled !== 'boolean') return
  return { name: value.name, code: value.code, description: value.description, is_enabled: value.is_enabled, protocol_endpoint_ids: endpoints, network_entry_ids: entries }
}
export function nodeGroupBaseline(value: any): string {
  const fields = pickNodeGroupFields({ ...value, description: value?.description || '', is_enabled: Boolean(value?.is_enabled), protocol_endpoint_ids: value?.protocol_endpoint_ids || [], network_entry_ids: value?.network_entry_ids || [] })
  return fields ? JSON.stringify({ ...fields, protocol_endpoint_ids: [...fields.protocol_endpoint_ids].sort((a,b) => a-b), network_entry_ids: [...fields.network_entry_ids].sort((a,b) => a-b) }) : ''
}
export interface NodeGroupRecovery { entityID: number; revision: number; baseline: string; originalEndpointIDs: number[]; originalEntryIDs: number[]; fields: NodeGroupFields }
export function nodeGroupRecovery(value: any): NodeGroupRecovery | undefined {
  const fields = pickNodeGroupFields(value?.fields), endpoints = ids(value?.originalEndpointIDs), entries = ids(value?.originalEntryIDs)
  if (!fields || !endpoints || !entries || !Number.isSafeInteger(value?.entityID) || value.entityID < 0 || !Number.isSafeInteger(value.revision) || value.revision < 0 || typeof value.baseline !== 'string' || value.baseline.length > 64 * 1024) return
  if (value.entityID > 0 && !value.baseline) return
  return { entityID: value.entityID, revision: value.revision, baseline: value.baseline, originalEndpointIDs: endpoints, originalEntryIDs: entries, fields }
}
