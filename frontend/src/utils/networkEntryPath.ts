export type PathProtocol = 'socks5' | 'shadowsocks' | 'vmess' | 'vless' | 'trojan' | 'hysteria2' | 'mieru'

export const pathProtocolOptions = [
  { label: 'Shadowsocks', value: 'shadowsocks' }, { label: 'VMess', value: 'vmess' },
  { label: 'VLESS', value: 'vless' }, { label: 'Trojan', value: 'trojan' },
  { label: 'Hysteria2', value: 'hysteria2' }, { label: 'SOCKS5', value: 'socks5' },
  { label: 'Mieru', value: 'mieru' },
]

export function emptyPathNode() {
  return { protocol: 'shadowsocks' as PathProtocol, server: '', port: 443, username: '', password: '', id: '',
    cipher: 'chacha20-ietf-poly1305', security: 'none', serverName: '', insecure: false, fingerprint: '',
    publicKey: '', shortID: '', flow: '', transport: 'tcp', path: '/', host: '', serviceName: '' }
}
export type PathNodeForm = ReturnType<typeof emptyPathNode>

export function buildPathNode(form: PathNodeForm, validate = true): Record<string, unknown> {
  if (validate) {
    if (!form.server.trim()) throw new Error('请填写代理节点地址。')
    if (!Number.isInteger(form.port) || form.port < 1 || form.port > 65535) throw new Error('代理节点端口必须为 1–65535。')
    if (['vless', 'vmess'].includes(form.protocol) && !form.id.trim()) throw new Error('请填写代理节点 UUID。')
    if (['shadowsocks', 'trojan', 'hysteria2', 'mieru'].includes(form.protocol) && !form.password) throw new Error('请填写代理节点密码。')
    if (form.protocol === 'vless' && form.security === 'reality' && !form.publicKey.trim()) throw new Error('请填写 Reality 公钥。')
    if (form.transport === 'grpc' && ['vless', 'vmess'].includes(form.protocol) && !form.serviceName.trim()) throw new Error('请填写 gRPC 服务名。')
  }
  const protocol: Record<string, unknown> = { type: form.protocol, server: form.server.trim(), port: form.port }
  if (['vless', 'vmess'].includes(form.protocol)) protocol.id = form.id.trim()
  if (['shadowsocks', 'trojan', 'hysteria2', 'mieru', 'socks5'].includes(form.protocol) && form.password) protocol.password = form.password
  if (['socks5', 'mieru'].includes(form.protocol) && form.username.trim()) protocol.username = form.username.trim()
  if (['shadowsocks', 'vmess'].includes(form.protocol)) protocol.cipher = form.cipher.trim()
  if (form.protocol === 'vless' && form.flow) protocol.flow = form.flow
  if (['vless', 'vmess'].includes(form.protocol)) {
    if (form.security === 'tls') protocol.tls = {
      ...(form.serverName.trim() ? { server_name: form.serverName.trim() } : {}), insecure: form.insecure,
      ...(form.fingerprint.trim() ? { client_fingerprint: form.fingerprint.trim() } : {}),
    }
    if (form.protocol === 'vless' && form.security === 'reality') protocol.reality = {
      public_key: form.publicKey.trim(), short_id: form.shortID.trim(),
      ...(form.serverName.trim() ? { server_name: form.serverName.trim() } : {}),
      ...(form.fingerprint.trim() ? { client_fingerprint: form.fingerprint.trim() } : {}),
    }
    if (form.transport === 'ws') protocol.ws = { path: form.path || '/', headers: form.host.trim() ? { Host: form.host.trim() } : {} }
    if (form.transport === 'grpc') protocol.grpc = { service_names: form.serviceName.split(',').map(s => s.trim()).filter(Boolean) }
  }
  if (['trojan', 'hysteria2'].includes(form.protocol)) {
    protocol.insecure = form.insecure
    if (form.fingerprint.trim()) protocol.client_fingerprint = form.fingerprint.trim()
    if (form.protocol === 'trojan' && form.serverName.trim()) protocol.sni = form.serverName.trim()
  }
  return { outbounds: [{ tag: 'proxy', protocol }], outbound_groups: [], target: 'proxy' }
}

// Raw is a complete replacement, not a lossy merge through the form schema.
// Accept a protocol, a single outbound, or the complete path graph.
export function parseRawPath(raw: string): Record<string, unknown> {
  let value: Record<string, unknown>
  try { value = JSON.parse(raw) } catch { throw new Error('Raw 配置必须是有效 JSON。') }
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).length === 0) throw new Error('Raw 配置必须是非空 JSON 对象。')
  if (typeof value.type === 'string') return { outbounds: [{ tag: 'proxy', protocol: value }], outbound_groups: [], target: 'proxy' }
  if (value.protocol && typeof value.protocol === 'object' && !Array.isArray(value.protocol)) {
    const tag = typeof value.tag === 'string' && value.tag.trim() ? value.tag : 'proxy'
    return { outbounds: [{ ...value, tag }], outbound_groups: [], target: tag }
  }
  if (!Array.isArray(value.outbounds) || typeof value.target !== 'string' || !value.target.trim()) throw new Error('完整路径需要 outbounds 和 target；也可以直接填写单个协议节点对象。')
  return value
}
