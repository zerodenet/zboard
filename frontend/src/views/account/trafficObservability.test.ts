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
})
