import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const routerSource = readFileSync(join(import.meta.dirname, 'index.ts'), 'utf8')
const mainSource = readFileSync(join(import.meta.dirname, '..', 'main.ts'), 'utf8')
const footerSource = readFileSync(join(import.meta.dirname, '..', 'layouts', 'PublicLayout.vue'), 'utf8')

describe('public document navigation', () => {
  it('uses the extensible docs prefix while preserving legacy policy URLs', () => {
    expect(routerSource).toContain("path: 'docs/:slug?'")
    expect(routerSource).toContain("path: 'terms', redirect: '/docs/terms'")
    expect(routerSource).toContain("path: 'privacy', redirect: '/docs/privacy'")
    expect(routerSource).toContain("path: 'refund', redirect: '/docs/refund'")
    expect(footerSource).toContain('`/docs/${document.slug}`')
  })

  it('returns routed documents to the top while respecting browser restoration', () => {
    expect(mainSource).toContain('scrollBehavior(_to, _from, savedPosition)')
    expect(mainSource).toContain('return savedPosition || { top: 0 }')
  })
})
