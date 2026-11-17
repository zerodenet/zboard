import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const routerSource = readFileSync(join(import.meta.dirname, 'index.ts'), 'utf8')

describe('route loading policy', () => {
  it('keeps page and layout modules out of the initial application chunk', () => {
    expect(routerSource).not.toMatch(/^import\s+.+\s+from\s+['"]\.\.\/(?:views|layouts)\//m)
    const components = routerSource.match(/\bcomponent\s*:/g) || []
    const lazyComponents = routerSource.match(/component:\s*\(\)\s*=>\s*import\(['"]\.\.\/(?:views|layouts)\//g) || []
    expect(components.length).toBeGreaterThan(0)
    expect(lazyComponents).toHaveLength(components.length)
  })
})
