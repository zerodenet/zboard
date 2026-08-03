import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const source = (...parts: string[]) => readFileSync(resolve(root, ...parts), 'utf8')

describe('protocol business load and Reality assistance', () => {
  it('shows subscriber-scoped protocol activity and exact admin activity counts', () => {
    const account = source('views', 'account', 'AccountSubscription.vue')
    const admin = source('views', 'Protocols.vue')
    expect(account).toContain('fetchAccountProtocolLoads')
    expect(account).toContain('协议实时负载')
    expect(account).toContain('item.active_users')
    expect(account).toContain('item.active_flows')
    expect(admin).toContain('endpoint.usage?.active_users')
    expect(admin).toContain('endpoint.usage?.active_flows')
  })

  it('offers one-click Reality scenarios and distinguishes host resources', () => {
    const protocols = source('views', 'Protocols.vue')
    const nodes = source('views', 'Nodes.vue')
    expect(protocols).toContain('generateRealityTemplate')
    expect(protocols).toContain('一键场景模板')
    expect(nodes).toContain('主机资源')
    expect(nodes).not.toContain('<strong>主机负载</strong>')
  })
})
