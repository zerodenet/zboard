import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const clientSource = readFileSync(
  join(import.meta.dirname, '..', 'api', 'client.ts'),
  'utf8',
)

describe('admin API request deadline policy', () => {
  it('does not abort ordinary admin operations at the old eight-second deadline', () => {
    expect(clientSource).toContain('timeout: 30_000')
    expect(clientSource).not.toContain('timeout: 8000')
  })

  it('keeps explicit longer deadlines for long-running deployment operations', () => {
    expect(clientSource).toContain('{ timeout: 120_000 }')
    expect(clientSource).toContain('{ timeout: 300_000 }')
  })
})
