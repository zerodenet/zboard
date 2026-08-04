import { readFileSync, readdirSync } from 'node:fs'
import { extname, join, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoots = [
  resolve(process.cwd(), 'src/views'),
  resolve(process.cwd(), 'src/components'),
]

function vueFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap(entry => {
    const path = join(root, entry.name)
    if (entry.isDirectory()) return vueFiles(path)
    return extname(entry.name) === '.vue' ? [path] : []
  })
}

function actionCellBodies(source: string): string[] {
  return Array.from(
    source.matchAll(/<td\b[^>]*class="[^"]*table-action-column[^"]*"[^>]*>([\s\S]*?)<\/td>/g),
    match => match[1] || '',
  )
}

function actionControlCount(source: string): number {
  return Array.from(source.matchAll(/<(?:UiButton|RouterLink|button)\b/g)).length
}

describe('administration table row actions', () => {
  const files = sourceRoots.flatMap(vueFiles)

  it('routes every multi-action table cell through the shared RowActions component', () => {
    const violations: string[] = []

    for (const file of files) {
      const source = readFileSync(file, 'utf8')
      for (const body of actionCellBodies(source)) {
        if (actionControlCount(body) >= 2 && !body.includes('<RowActions')) {
          violations.push(file.replace(`${process.cwd()}/`, ''))
        }
      }
    }

    expect(violations).toEqual([])
  })

  it('does not reintroduce wrapping page-local action groups', () => {
    const violations = files
      .filter(file => !file.endsWith('/RowActions.vue'))
      .filter(file => /inline-actions|action-buttons|table-actions/.test(readFileSync(file, 'utf8')))
      .map(file => file.replace(`${process.cwd()}/`, ''))

    expect(violations).toEqual([])
  })

  it('keeps destructive actions last inside shared row-action menus', () => {
    const violations: string[] = []

    for (const file of files) {
      const source = readFileSync(file, 'utf8')
      for (const match of source.matchAll(/<RowActions\b[^>]*>([\s\S]*?)<\/RowActions>/g)) {
        const body = match[1] || ''
        const dangerIndex = Math.max(body.lastIndexOf('variant="danger"'), body.lastIndexOf('button-danger'))
        if (dangerIndex < 0) continue
        if (/<(?:UiButton|RouterLink|button)\b/.test(body.slice(dangerIndex))) {
          violations.push(file.replace(`${process.cwd()}/`, ''))
        }
      }
    }

    expect(violations).toEqual([])
  })
})
