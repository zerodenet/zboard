import { readFileSync, readdirSync } from 'node:fs'
import { extname, join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(import.meta.dirname, '..')
const tokenPath = join(import.meta.dirname, 'tokens.css')

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return sourceFiles(path)
    return ['.css', '.ts', '.vue'].includes(extname(path)) ? [path] : []
  })
}

describe('design token ownership', () => {
  it('keeps raw color literals in the token source only', () => {
    const rawColor = /#[\da-f]{3,8}\b|(?:rgb|rgba|hsl|hsla)\([^)]*\)/gi
    const violations = sourceFiles(sourceRoot)
      .filter(path => path !== tokenPath && path !== import.meta.filename)
      .flatMap(path => {
        const matches = readFileSync(path, 'utf8').match(rawColor) || []
        return matches.map(value => `${relative(sourceRoot, path)}: ${value}`)
      })

    expect(violations).toEqual([])
  })

  it('declares every referenced CSS custom property', () => {
    const declared = new Set(Array.from(readFileSync(tokenPath, 'utf8').matchAll(/\s(--[a-z0-9-]+)\s*:/gi), match => match[1]))
    const runtimeProperties = new Set(['--metric-columns'])
    const missing = new Set<string>()
    for (const path of sourceFiles(sourceRoot)) {
      if (path === tokenPath) continue
      for (const match of readFileSync(path, 'utf8').matchAll(/var\((--[a-z0-9-]+)/gi)) {
        if (!declared.has(match[1]) && !runtimeProperties.has(match[1])) missing.add(match[1])
      }
    }

    expect([...missing].sort()).toEqual([])
  })

  it('keeps page-shell styles in their owned stylesheets', () => {
    const baseStyles = readFileSync(join(sourceRoot, 'styles.css'), 'utf8')
    const authStyles = readFileSync(join(sourceRoot, 'styles', 'auth.css'), 'utf8')
    const publicStyles = readFileSync(join(sourceRoot, 'styles', 'public.css'), 'utf8')
    const accountStyles = readFileSync(join(sourceRoot, 'styles', 'account.css'), 'utf8')

    expect(baseStyles).not.toMatch(/\.(?:auth|public)-(?:shell|header|main|aside)\b/)
    expect(baseStyles).not.toMatch(/\.account-(?:shell|header|identity|content|metric|plan|sku|notice)\b/)
    expect(authStyles).not.toMatch(/\.(?:public|account)-(?:shell|header)\b/)
    expect(publicStyles).not.toMatch(/\.(?:auth|account)-(?:shell|header)\b/)
    expect(accountStyles).not.toMatch(/\.(?:auth|public)-(?:shell|header)\b/)
  })

  it('keeps mobile workbench filters at their natural content height', () => {
    const baseStyles = readFileSync(join(sourceRoot, 'styles.css'), 'utf8')
    const mobileWorkbenchRule = baseStyles.match(/@media\(max-width:720px\)\{[\s\S]*?\.workbench-filters\{([^}]*)\}/)?.[1] || ''

    expect(mobileWorkbenchRule).toContain('flex:0 0 auto')
    expect(mobileWorkbenchRule).toContain('width:100%')
    expect(mobileWorkbenchRule).toContain('align-content:flex-start')
    expect(baseStyles).toContain(".data-table{width:max-content;min-width:100%!important}")
    expect(baseStyles).toContain('.detail-drawer { min-width:0; min-height:0; height:100%;')
    expect(baseStyles).toContain('overflow:hidden; background:var(--surface); box-shadow:-18px 0 48px var(--drawer-shadow); }')
  })
})
