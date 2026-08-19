import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const accountViews = join(import.meta.dirname, '..', 'views', 'account')

function source(name: string) {
  return readFileSync(join(accountViews, name), 'utf8')
}

describe('account list scale policy', () => {
  it('uses bounded server pages for orders and subscriptions', () => {
    const orders = source('AccountOrders.vue')
    const subscriptions = source('AccountSubscription.vue')

    expect(orders).toContain('fetchAccountOrdersPage')
    expect(orders).toContain('useRemoteTable')
    expect(orders).toContain('<TablePager')
    expect(orders).not.toMatch(/\bfetchOrders\(/)

    expect(subscriptions).toContain('fetchAccountSubscriptionsPage')
    expect(subscriptions).toContain('useRemoteTable')
    expect(subscriptions).toContain('<TablePager')
    expect(subscriptions).not.toMatch(/\bfetchSubscriptions\(/)
    expect(subscriptions).not.toMatch(/\bfetchPlans\(/)
  })

  it('uses bounded cursor history with URL-restorable time filters for traffic', () => {
    const traffic = source('AccountTraffic.vue')

    expect(traffic).toContain('fetchTrafficUsagePage')
    expect(traffic).toContain('fetchTrafficNodeSeries')
    expect(traffic).toContain('useCursorTable')
    expect(traffic).toContain('<CursorPager')
    expect(traffic).toContain('<WorkbenchFilterDate')
    expect(traffic).toContain('TrafficUsageBucket')
    expect(traffic).not.toMatch(/\bfetchTrafficRecords\(/)
  })

  it('keeps dashboard previews and totals bounded on the server', () => {
    const dashboard = source('AccountDashboard.vue')

    expect(dashboard).toContain('fetchAccountOrdersPage')
    expect(dashboard).toContain('fetchAccountSubscriptionsPage')
    expect(dashboard).toContain("limit: 3")
    expect(dashboard).toContain("limit: 1")
    expect(dashboard).not.toMatch(/\bfetchOrders\(/)
    expect(dashboard).not.toMatch(/\bfetchSubscriptions\(/)
    expect(dashboard).not.toMatch(/\bfetchPlans\(/)
  })
})
