import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const routerSource = readFileSync(resolve(process.cwd(), '../backend/internal/server/router.go'), 'utf8')

// This source-level guard keeps the shipped UI route bound to the aggregated
// read model instead of accidentally reverting to raw accounting rows.
describe('traffic history route', () => {
  it('uses the human-facing usage aggregation handler', () => {
    expect(routerSource).toContain('"/api/v1/traffic/records", h.TrafficUsageRecordsHandler')
    expect(routerSource).toContain('"/api/v1/admin/traffic/records", h.TrafficUsageRecordsHandler')
  })
})
