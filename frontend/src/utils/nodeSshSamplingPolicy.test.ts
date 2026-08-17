import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const nodesSource = readFileSync(
  join(import.meta.dirname, '..', 'views', 'Nodes.vue'),
  'utf8',
)

describe('node SSH resource sampling policy', () => {
  it('keeps host resource sampling behind the explicit refresh action', () => {
    expect(nodesSource).toContain('@click="loadNodeLoad(selectedNode.id)"')
    expect(nodesSource).toContain('页面不会自动采样')
  })

  it('does not bind host resource sampling to node detail lifecycle watchers', () => {
    const watcherStart = nodesSource.indexOf(
      'watch([() => selectedNode.value?.id, detailSection, nodeProtocolOffset, nodeProtocolLimit]',
    )
    expect(watcherStart).toBeGreaterThanOrEqual(0)

    const watcherEnd = nodesSource.indexOf('watch(detailSection,', watcherStart)
    expect(watcherEnd).toBeGreaterThan(watcherStart)

    const watcherSource = nodesSource.slice(watcherStart, watcherEnd)
    expect(watcherSource).not.toContain('loadNodeLoad(')
  })
})
