// View-specific ordinary fields only. Never put credentials or arbitrary config
// into a baseline or draft; fresh authorized reads supply unchanged stored data.
const object = (v: any) => v && typeof v === 'object' && !Array.isArray(v)
const id = (v: any) => Number.isSafeInteger(v) && v >= 0
const text = (v: any, limit: number) => typeof v === 'string' && new TextEncoder().encode(v).byteLength <= limit
const status = (v: any) => ['active', 'suspended', 'deactivated'].includes(v)
const version = (v: any) => text(v, 64) && v.length > 0 && Number.isFinite(Date.parse(v))
export function userAccountFields(v: any) {
  if (!object(v) || !text(v.email, 128) || !status(v.status) || typeof v.is_admin !== 'boolean') return
  return { email: v.email as string, status: v.status as string, is_admin: v.is_admin as boolean }
}
export function userAccountBaseline(v: any) {
  const fields = userAccountFields(v)
  if (!fields || !id(v.id) || !v.id || !version(v.updated_at)) return
  return { id: v.id as number, updated_at: v.updated_at as string, ...fields }
}
export function userAccountRecovery(v: any) {
  const fields = userAccountFields(v?.fields)
  if (!fields || !id(v?.id)) return
  const baseline = v.id ? userAccountBaseline(v.baseline) : null
  if (v.id && (!baseline || baseline.id !== v.id || fields.email !== baseline.email) || !v.id && v.baseline !== null) return
  return { id: v.id as number, fields, baseline }
}
export function providerFields(v: any) {
  if (!object(v) || !text(v.provider_key, 80) || !v.provider_key || !text(v.name, 320)) return
  return { provider_key: v.provider_key as string, name: v.name as string }
}
export function providerBaseline(v: any) {
  const fields = providerFields(v)
  if (!fields || !id(v.id) || !v.id || !id(v.revision) || !v.revision) return
  return { id: v.id as number, revision: v.revision as number, ...fields }
}
export function providerRecovery(v: any) {
  const fields = providerFields(v?.fields)
  if (!fields || !id(v?.id)) return
  const baseline = v.id ? providerBaseline(v.baseline) : null
  if (v.id && (!baseline || baseline.id !== v.id || baseline.provider_key !== fields.provider_key) || !v.id && v.baseline !== null) return
  return { id: v.id as number, fields, baseline }
}
function memberships(v: any) {
  if (!Array.isArray(v) || v.length > 1000) return
  const result: { node_group_id: number; revision: number; name: string; code: string; description: string; is_enabled: boolean; sort_order: number }[] = []
  for (const item of v) {
    if (!object(item) || !id(item.node_group_id) || !item.node_group_id || !id(item.revision) || !item.revision || result.some(x => x.node_group_id === item.node_group_id)) return
    if (!text(item.name, 1024) || !text(item.code, 1024) || !text(item.description, 4096) || typeof item.is_enabled !== 'boolean' || !Number.isSafeInteger(item.sort_order)) return
    result.push({ node_group_id: item.node_group_id, revision: item.revision, name: item.name, code: item.code, description: item.description, is_enabled: item.is_enabled, sort_order: item.sort_order })
  }
  return result.sort((a, b) => a.node_group_id - b.node_group_id)
}
export function protocolAssetFields(v: any) {
  if (!object(v) || !id(v.node_id) || !text(v.name, 1024) || !text(v.address, 4096) || !text(v.listen_address, 4096)
    || !Number.isInteger(v.port) || v.port < 1 || v.port > 65535 || !Number.isInteger(v.public_port) || v.public_port < 1 || v.public_port > 65535) return
  const groups = memberships(v.node_group_memberships)
  if (!groups) return
  return { node_id: v.node_id as number, name: v.name as string, address: v.address as string, listen_address: v.listen_address as string, port: v.port as number, public_port: v.public_port as number, node_group_memberships: groups }
}
export function protocolAssetBaseline(v: any) {
  const fields = protocolAssetFields({ ...v, listen_address: v?.listen_address || '0.0.0.0', node_group_memberships: v?.node_group_memberships || [] })
  if (!fields || !id(v.id) || !v.id || !version(v.updated_at) || !text(v.protocol, 80)) return
  return { id: v.id as number, updated_at: v.updated_at as string, protocol: v.protocol as string, ...fields }
}
export function protocolAssetRecovery(v: any) {
  const fields = protocolAssetFields(v?.fields)
  if (!fields || !id(v?.id)) return
  const baseline = v.id ? protocolAssetBaseline(v.baseline) : null
  if (v.id && (!baseline || baseline.id !== v.id) || !v.id && v.baseline !== null) return
  return { id: v.id as number, fields, baseline }
}
export function sameRecoveryBaseline(left: unknown, right: unknown) { return left != null && right != null && JSON.stringify(left) === JSON.stringify(right) }
