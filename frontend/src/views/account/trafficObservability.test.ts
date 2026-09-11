import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function readSource(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('account traffic observability', () => {
  it('uses the backend trend read model instead of scanning record pages', () => {
    const source = readSource('./trafficObservability.ts')
    expect(source).toContain('fetchTrafficTrends')
    expect(source).not.toContain('fetchAccountTrafficRecordsPage')
    expect(source).not.toContain('while (')
    expect(source).not.toContain('5000')
  })

  it('keeps missing connection samples unavailable', () => {
    const source = readSource('./trafficObservability.ts')
    expect(source).toContain('peak_connections: null')
    expect(source).toContain('connection_sample_count: 0')
  })

  it('uses user-facing empty copy instead of internal diagnostics or a future placeholder', () => {
    const chart = readSource('./TrafficObservabilityChart.vue')
    const page = readSource('./AccountTraffic.vue')
    expect(chart).toContain('暂无连接数据')
    expect(chart).toContain('所选时间范围内暂时没有可用的连接统计')
    expect(chart).not.toContain('请检查节点内核版本和事件上报链路')
    expect(chart).not.toContain('字段已经预留')
    expect(page).toContain('Principal 当前态观测')
    expect(page).not.toContain('按发生时间重放并聚合')
  })
})
