import { describe, expect, it } from 'vitest'
import { buildPathNode, emptyPathNode, parseRawPath, pathProtocolOptions } from './networkEntryPath'

describe('network entry proxy paths', () => {
  it('builds a single SS hop without changing the secret', () => {
    const form = { ...emptyPathNode(), server: 'ss.example.com', port: 8388, password: ' literal password ' }
    expect(buildPathNode(form)).toEqual({ outbounds: [{ tag: 'proxy', protocol: { type: 'shadowsocks', server: 'ss.example.com', port: 8388, password: ' literal password ', cipher: 'chacha20-ietf-poly1305' } }], outbound_groups: [], target: 'proxy' })
  })
  it('emits protocol-owned TLS, Reality, WS and gRPC shapes', () => {
    const form = { ...emptyPathNode(), protocol: 'vless' as const, server: 'proxy.example.com', id: 'uuid', security: 'reality', publicKey: 'public-key', shortID: 'aabb', serverName: 'tls.example.com', transport: 'ws', host: 'ws.example.com', path: '/proxy' }
    expect(buildPathNode(form)).toMatchObject({ outbounds: [{ protocol: { reality: { public_key: 'public-key', short_id: 'aabb', server_name: 'tls.example.com' }, ws: { path: '/proxy', headers: { Host: 'ws.example.com' } } } }] })
    const vmess = { ...form, protocol: 'vmess' as const, cipher: 'aes-128-gcm', security: 'tls', transport: 'grpc', serviceName: 'one,two' }
    const result = buildPathNode(vmess) as any
    expect(result.outbounds[0].protocol).toMatchObject({ type: 'vmess', cipher: 'aes-128-gcm', tls: { server_name: 'tls.example.com' }, grpc: { service_names: ['one', 'two'] } })
    expect(result.outbounds[0].protocol).not.toHaveProperty('reality')
  })
  it('accepts every current proxy protocol in the single node form', () => {
    for (const option of pathProtocolOptions) {
      const form = { ...emptyPathNode(), protocol: option.value as ReturnType<typeof emptyPathNode>['protocol'], server: 'proxy.example.com', id: 'uuid', password: 'secret' }
      expect(buildPathNode(form)).toMatchObject({ outbounds: [{ protocol: { type: option.value } }] })
    }
  })
  it('preserves arbitrary kernel fields and full group graphs in raw overrides', () => {
    const node = { type: 'vless', server: 'proxy.example.com', port: 443, id: 'uuid', split_http: { path: '/stream', mode: 'auto' }, mux_concurrency: 8 }
    expect(parseRawPath(JSON.stringify(node))).toMatchObject({ outbounds: [{ protocol: node }], target: 'proxy' })
    const outbound = { tag: 'custom', protocol: node, udp: { enabled: false } }
    expect(parseRawPath(JSON.stringify(outbound))).toEqual({ outbounds: [outbound], outbound_groups: [], target: 'custom' })
    const graph = { outbounds: [outbound], outbound_groups: [{ tag: 'pick', type: 'selector', outbounds: ['custom'], default: 'custom' }], target: 'pick' }
    expect(parseRawPath(JSON.stringify(graph))).toEqual(graph)
  })
  it('rejects empty and malformed drafts before replacing a saved path', () => {
    for (const raw of ['', 'null', '[]', '{}', '{bad', '{"target":"missing"}']) expect(() => parseRawPath(raw)).toThrow()
    expect(() => buildPathNode(emptyPathNode())).toThrow('地址')
    expect(() => buildPathNode({ ...emptyPathNode(), server: 'host', port: 0 })).toThrow('端口')
  })
})
