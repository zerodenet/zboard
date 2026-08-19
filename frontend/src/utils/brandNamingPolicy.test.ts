import { readFileSync, readdirSync } from 'node:fs'
import { extname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const frontendRoot = join(import.meta.dirname, '../..')
const sourceRoot = join(frontendRoot, 'src')
const repositoryRoot = join(frontendRoot, '..')
const wrongBrand = new RegExp(`\\b${'Z' + 'board'}\\b`)
const sourceExtensions = new Set(['.ts', '.vue'])

function collectSourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return collectSourceFiles(path)
    if (!sourceExtensions.has(extname(entry.name))) return []
    if (entry.name === 'brandNamingPolicy.test.ts') return []
    return [path]
  })
}

describe('ZBoard display naming policy', () => {
  it('uses the canonical ZBoard capitalization in human-facing source and project docs', () => {
    const files = [
      ...collectSourceFiles(sourceRoot),
      join(frontendRoot, 'index.html'),
      ...['README.md', 'README.zh-CN.md', 'CONTRIBUTING.md', 'SECURITY.md', 'RELEASING.md'].map(name => join(repositoryRoot, name)),
    ]
    const violations = files.flatMap(file => {
      let content = ''
      try { content = readFileSync(file, 'utf8') } catch { return [] }
      return wrongBrand.test(content) ? [file.replace(`${repositoryRoot}/`, '')] : []
    })
    expect(violations).toEqual([])
  })
})
