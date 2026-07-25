import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const views = join(import.meta.dirname, '..', 'views')

describe('infrastructure detail density policy', () => {
  it('uses a comparison table for node protocol multipliers', () => {
    const nodes = readFileSync(join(views, 'Nodes.vue'), 'utf8')

    expect(nodes).toContain('caption="当前节点协议服务与计费倍率"')
    expect(nodes).toContain(':row-count="nodeProtocolTotal"')
    expect(nodes).not.toContain('<article v-for="endpoint in nodeEndpoints"')
  })

  it('uses a comparison table for protocol deployment history', () => {
    const protocols = readFileSync(join(views, 'Protocols.vue'), 'utf8')

    expect(protocols).toContain('caption="协议服务发布历史"')
    expect(protocols).toContain(':row-count="deploymentTotal"')
    expect(protocols).not.toContain('<article v-for="item in deployments"')
  })
})
