import { readFileSync, readdirSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(import.meta.dirname, '..')

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return vueFiles(path)
    return entry.name.endsWith('.vue') ? [path] : []
  })
}

describe('application feedback policy', () => {
  it('routes success results to Toast and keeps errors in persistent live regions', () => {
    const violations = vueFiles(sourceRoot).flatMap(path => {
      const source = readFileSync(path, 'utf8')
      const file = relative(sourceRoot, path)
      const items: string[] = []

      if (/\bmessage\.value\s*=/.test(source) && !/<TransientFeedback\b[^>]*:success=/.test(source)) {
        items.push(`${file}: success result is not connected to TransientFeedback`)
      }
      if (
        /\berror\.value\s*=/.test(source)
        && !/<(?:TransientFeedback|PageAlert)\b[^>]*(?::error=|v-if=)/.test(source)
        && !/role="alert"/.test(source)
      ) {
        items.push(`${file}: error result has no persistent alert region`)
      }
      return items
    })

    expect(violations).toEqual([])
  })

  it('does not bypass shared confirmation and feedback with native dialogs', () => {
    const violations = vueFiles(sourceRoot).flatMap(path => {
      const source = readFileSync(path, 'utf8')
      const matches = source.match(/\b(?:window\.)?(?:alert|confirm|prompt)\s*\(/g) || []
      return matches.map(value => `${relative(sourceRoot, path)}: ${value}`)
    })

    expect(violations).toEqual([])
  })
})
