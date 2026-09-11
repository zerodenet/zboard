import { buildPathNode, emptyPathNode, type PathNodeForm } from './networkEntryPath'

export type JSONObject = Record<string, any>
export interface ProxyPoolGraph { outbounds: JSONObject[]; outbound_groups: JSONObject[]; target: string }
export const cloneJSON = <T>(value: T): T => JSON.parse(JSON.stringify(value))
const object = (v: any): v is JSONObject => !!v && typeof v === 'object' && !Array.isArray(v)

export function equalJSON(a: any, b: any): boolean {
  if (a === b) return true
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((item, index) => equalJSON(item, b[index]))
  if (object(a) && object(b)) return Object.keys(a).length === Object.keys(b).length && Object.keys(a).every(key => Object.prototype.hasOwnProperty.call(b, key) && equalJSON(a[key], b[key]))
  return false
}

export function parseProxyPoolGraph(raw: string): ProxyPoolGraph {
  let value: any
  try { value = JSON.parse(raw) } catch { throw new Error('完整 RAW 必须是有效的 Zero JSON。') }
  validateProxyPoolGraph(value)
  return { ...value, outbound_groups: value.outbound_groups || [] }
}
export function validateProxyPoolGraph(value: any): asserts value is ProxyPoolGraph {
  if (!object(value) || Object.keys(value).some(key => !['outbounds', 'outbound_groups', 'target'].includes(key))) throw new Error('代理池只接受 outbounds、outbound_groups 和 target。')
  if (!Array.isArray(value.outbounds) || (value.outbound_groups !== undefined && !Array.isArray(value.outbound_groups))) throw new Error('outbounds 和 outbound_groups 必须是数组。')
  if (new TextEncoder().encode(JSON.stringify(value)).length > 128 * 1024) throw new Error('代理池配置不能超过 128 KiB。')
  const groups: JSONObject[] = value.outbound_groups || [], tags = new Set<string>()
  for (const item of [...value.outbounds, ...groups]) {
    if (!object(item) || typeof item.tag !== 'string' || !item.tag.trim() || item.tag !== item.tag.trim()) throw new Error('每个代理和分组都需要非空标识，且不能以空格开头或结尾。')
    if (tags.has(item.tag)) throw new Error(`标识重复：${item.tag}`)
    tags.add(item.tag)
  }
  for (const item of value.outbounds) if (!object(item.protocol) || typeof item.protocol.type !== 'string') throw new Error(`代理 ${item.tag} 缺少 protocol.type。`)
  if (typeof value.target !== 'string' || !tags.has(value.target)) throw new Error('请选择有效的代理池出口 target。')
  const edges = new Map<string, string[]>()
  for (const group of groups) {
    if (!['selector', 'url_test', 'relay'].includes(group.type)) throw new Error(`分组 ${group.tag} 的类型必须是 selector、url_test 或 relay。`)
    const members = group[group.type === 'relay' ? 'proxies' : 'outbounds']
    if (!Array.isArray(members) || !members.length || members.some(tag => typeof tag !== 'string' || !tags.has(tag))) throw new Error(`分组 ${group.tag} 需要至少一个有效成员。`)
    for (const key of ['selected', 'default']) if (group[key] !== undefined && !members.includes(group[key])) throw new Error(`分组 ${group.tag} 的 ${key} 必须属于组内成员。`)
    edges.set(group.tag, members)
  }
  const visited = new Set<string>(), pending = new Set<string>()
  function visit(tag: string) {
    if (pending.has(tag)) throw new Error('分组之间不能循环引用。')
    if (visited.has(tag)) return
    pending.add(tag); (edges.get(tag) || []).forEach(visit); pending.delete(tag); visited.add(tag)
  }
  tags.forEach(visit)
}

export function hydratePathNode(outbound: JSONObject): PathNodeForm {
  const p = outbound.protocol, form = emptyPathNode()
  Object.assign(form, { protocol: p.type, server: p.server ?? '', port: p.port ?? 443, username: p.username ?? '', password: p.password ?? '', id: p.id ?? '', cipher: p.cipher ?? form.cipher, flow: p.flow ?? '' })
  const security = p.reality || p.tls || p
  Object.assign(form, { security: p.reality ? 'reality' : p.tls ? 'tls' : 'none', serverName: security.server_name ?? p.sni ?? '', insecure: security.insecure ?? false, fingerprint: security.client_fingerprint ?? '', publicKey: security.public_key ?? '', shortID: security.short_id ?? '', transport: p.ws ? 'ws' : p.grpc ? 'grpc' : 'tcp', path: p.ws?.path ?? '/', host: p.ws?.headers?.Host ?? '', serviceName: p.grpc?.service_names?.join(',') ?? '' })
  return form
}
// Apply only fields changed in the form. Unknown options, optional-field absence,
// credentials and transport extensions survive an unchanged round trip.
function patchChanges(original: JSONObject, before: JSONObject, after: JSONObject): JSONObject {
  const result = cloneJSON(original)
  for (const key of new Set([...Object.keys(before), ...Object.keys(after)])) {
    if (JSON.stringify(before[key]) === JSON.stringify(after[key])) continue
    if (!(key in after)) delete result[key]
    else if (object(before[key]) && object(after[key]) && object(result[key])) result[key] = patchChanges(result[key], before[key], after[key])
    else result[key] = cloneJSON(after[key])
  }
  return result
}
export function mergePathNode(outbound: JSONObject, initial: PathNodeForm, current: PathNodeForm, validate = true): JSONObject {
  const before = (buildPathNode(initial, false).outbounds as JSONObject[])[0]!.protocol
  const after = (buildPathNode(current, validate).outbounds as JSONObject[])[0]!.protocol
  return { ...cloneJSON(outbound), protocol: before.type === after.type ? patchChanges(outbound.protocol, before, after) : after }
}
