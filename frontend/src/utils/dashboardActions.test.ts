import { describe, expect, it } from 'vitest'
import { dashboardActionRoutes } from './dashboardActions'

describe('dashboard action routes', () => {
  it('only exposes drill-downs for current unresolved conditions', () => {
    expect(dashboardActionRoutes).toEqual({
      tickets: '/admin/tickets?status=attention',
      nodes: '/admin/nodes?connector=offline',
      deployments: '/admin/protocols?deployment=failed',
    })
  })
})
