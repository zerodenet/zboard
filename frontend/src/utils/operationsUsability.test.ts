import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = (path: string) => readFileSync(resolve(process.cwd(), path), 'utf8')
const fairUse = source('src/views/FairUse.vue')
const fairUseApi = source('src/api/fairUse.ts')
const templates = source('src/views/SubscriptionTemplates.vue')
const customizer = source('src/components/SubscriptionTemplateCustomizer.vue')
const traffic = source('src/views/Traffic.vue')
const trafficApi = source('src/api/trafficUsage.ts')
const nodeChart = source('src/views/account/NodeTrafficChart.vue')

describe('operations usability regressions', () => {
  it('loads Fair Use immediately after selecting a subscription and exposes observe-only evaluation controls', () => {
    const selectFlow = fairUse.match(/async function selectSubscription[\s\S]*?\n}/)?.[0] || ''
    expect(selectFlow).toContain('await syncURL()')
    expect(selectFlow).toContain('await load()')
    expect(fairUse).toContain('启用当前订阅评估')
    expect(fairUseApi).toContain("enforcement_mode: 'observe'")
    expect(fairUseApi).toContain('/fair-use/evaluate')
  })

  it('makes template ordering and availability direct administration actions', () => {
    expect(customizer).toContain('@click="movePolicyGroup(index, -1)"')
    expect(customizer).toContain('@click="movePolicyGroup(index, 1)"')
    expect(templates).toContain('@click="toggleActive(item)"')
    expect(templates).toContain('expected_revision: detail.revision')
  })

  it('supports daily traffic analysis and node-first ranking in the admin view', () => {
    expect(trafficApi).toContain("export type TrafficUsageBucket = 'minute' | 'hour' | 'day'")
    expect(traffic).toContain("{ label: '按天', value: 'day' }")
    expect(traffic).toContain('show-ranking')
    expect(traffic).toContain('查看昨日')
    expect(nodeChart).toContain('按所选日期范围内计费流量从高到低')
  })
})
