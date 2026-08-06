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
    expect(source).toContain('fetchSubscriptionAccess(selectedSubscriptionID.value)')
    expect(source).toContain('rotateSubscriptionAccess(subscriptionID)')
    expect(source).toContain('revokeSubscriptionAccess(subscriptionID)')
    expect(source).toContain('其他订阅不会被合并到该链接')
    expect(source).not.toContain('累计已用流量、总额和到期时间')
  })
})
