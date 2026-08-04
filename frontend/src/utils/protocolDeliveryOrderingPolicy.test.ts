import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const protocolsView = readFileSync(resolve(process.cwd(), 'src/views/Protocols.vue'), 'utf8')
const apiClient = readFileSync(resolve(process.cwd(), 'src/api/client.ts'), 'utf8')

describe('protocol delivery ordering policy', () => {
  it('uses a dedicated complete-scope command instead of the filtered table or endpoint editor', () => {
    expect(protocolsView).toContain('调整交付顺序')
    expect(protocolsView).toContain('不受当前分页、筛选或节点分组视图影响')
    expect(protocolsView).toContain('fetchProtocolEndpointOrder()')
    expect(protocolsView).toContain('updateProtocolEndpointOrder({')
    expect(protocolsView).not.toContain('label="排序" name="protocol-sort"')
  })

  it('keeps delivery ordering separate from node publication', () => {
    expect(apiClient).toContain("api.get('/admin/protocol-endpoints/order')")
    expect(apiClient).toContain("api.put('/admin/protocol-endpoints/order', payload)")
    expect(protocolsView).toContain('无需发布节点')
  })
})
