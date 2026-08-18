import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const client = readFileSync(join(import.meta.dirname, '..', 'api', 'client.ts'), 'utf8')
const nodes = readFileSync(join(import.meta.dirname, '..', 'views', 'Nodes.vue'), 'utf8')

describe('Zero kernel async operation policy', () => {
  it('does not hold the browser request for kernel reconcile', () => {
    expect(client).toContain('Promise<AdminTask>')
    expect(client).not.toContain('`/nodes/${nodeId}/kernel/reconcile`, options || {}, { timeout: 300_000 }')
    expect(nodes).toContain('trackAdminTask(task)')
    expect(nodes).toContain('startKernelPolling(nodeID, task.id)')
  })

  it('pins a target release for batch Zero rollout', () => {
    expect(nodes).toContain('title="批量升级 Zero"')
    expect(nodes).toContain("action: 'reconcile'")
    expect(nodes).toContain('version: batchReleaseVersion.value')
    expect(nodes).toContain('allow_downgrade: batchAllowDowngrade.value')
    expect(nodes).toContain('服务端最多并发处理 4 台')
  })
})
