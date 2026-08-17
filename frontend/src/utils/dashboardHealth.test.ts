import { describe, expect, it } from 'vitest'
import { buildDashboardAttention } from './dashboardHealth'

describe('dashboard operator attention', () => {
  it('does not turn historical task/order failures into current operator todos', () => {
    expect(buildDashboardAttention({
      tasks_failed: 12,
      orders_failed: 7,
      orders_pending: 4,
    })).toEqual([])
  })

  it('only exposes conditions that remain unresolved now', () => {
    const items = buildDashboardAttention({
      tickets_pending: 2,
      nodes_offline: 1,
      deployments_failed: 3,
      tasks_failed: 20,
      orders_failed: 10,
    })

    expect(items.map(item => [item.key, item.value])).toEqual([
      ['nodes', 1],
      ['deployments', 3],
      ['tickets', 2],
    ])
  })

  it('drops an attention item as soon as its current condition is healthy', () => {
    expect(buildDashboardAttention({
      tickets_pending: 0,
      nodes_offline: 0,
      deployments_failed: 0,
    })).toEqual([])
  })
})
