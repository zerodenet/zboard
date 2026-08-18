import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const nodesSource = readFileSync(
  join(import.meta.dirname, '..', 'views', 'Nodes.vue'),
  'utf8',
)

describe('node Connector status semantics', () => {
  it('does not label Connector reachability as Zero health', () => {
    expect(nodesSource).toContain('<th>Connector</th>')
    expect(nodesSource).toContain('label="Connector"')
    expect(nodesSource).toContain('label="Zero 内核"')
    expect(nodesSource).not.toContain('<th>Zero</th>')
  })

  it('distinguishes never connected from a previously connected offline node', () => {
    expect(nodesSource).toContain("node.connector_online ? '在线' : node.connector_last_seen_at ? '离线' : '未连接'")
    expect(nodesSource).toContain("selectedNode.connector_online ? '在线' : selectedNode.connector_last_seen_at ? '离线' : '未连接'")
  })
})
