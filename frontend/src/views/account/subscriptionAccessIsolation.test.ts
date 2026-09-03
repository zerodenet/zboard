import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function readSource(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('subscription access isolation source contract', () => {
  it('scopes every credential mutation to a subscription id', () => {
    const source = readSource('../../api/subscriptionAccess.ts')
    expect(source).toContain('/account/subscriptions/${subscriptionId}/access')
    expect(source).toContain('`${accessPath(subscriptionId)}/rotate`')
    expect(source).not.toContain("'/subscription/access'")
  })

  it('manages links from an explicitly selected subscription', () => {
    const source = readSource('./AccountSubscription.vue')
    expect(source).toContain('selectedSubscriptionID')
    expect(source).toContain('fetchSubscriptionAccess(subscriptionID, { signal })')
    expect(source).toContain('result.subscription_id !== subscriptionID')
    expect(source).toContain('rotateSubscriptionAccess(subscriptionID)')
    expect(source).toContain('revokeSubscriptionAccess(subscriptionID)')
    expect(source).toContain('其他订阅不会被合并到该链接')
    expect(source).not.toContain('累计已用流量、总额和到期时间')
  })

  it('describes built-in native delivery as Base64 encoding instead of plaintext JSON', () => {
    const source = readSource('./AccountSubscription.vue')
    expect(source).toContain('ZBoard 原生格式（Base64）')
    expect(source).toContain('Base64 编码的 ZBoard 原生配置')
    expect(source).not.toContain("label: 'ZBoard 原生 JSON'")
    expect(source).not.toContain('返回原生 JSON')
  })
})
