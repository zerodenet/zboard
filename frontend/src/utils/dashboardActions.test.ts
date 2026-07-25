import { describe, expect, it } from 'vitest'
import { dashboardActionRoutes } from './dashboardActions'

describe('dashboard action routes', () => {
  it('opens the exact server-backed result set represented by each counter', () => {
    expect(dashboardActionRoutes).toEqual({
      tickets: '/admin/tickets?status=attention',
      orders: '/admin/orders?status=attention',
      nodes: '/admin/nodes?connector=offline',
      tasks: '/admin/tasks?status=3',
      deployments: '/admin/protocols?deployment=failed',
    })
  })
})
