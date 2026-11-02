import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { parse } from 'vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

const root = join(import.meta.dirname, '..')
// Derive coverage from real admin routes, so a newly added page is checked too.
const adminRoutes = readFileSync(join(root, 'router/index.ts'), 'utf8').split("path: '/admin',")[1]
const pages = [...new Set(Array.from(adminRoutes.matchAll(/import\('\.\.\/views\/([^']+\.vue)'\)/g), match => match[1]))]

describe('admin UI standard v2', () => {
  it('keeps every routed admin template valid and uses shared controls', () => {
    const violations: string[] = []
    for (const page of pages) {
      const { descriptor, errors } = parse(readFileSync(join(root, 'views', page), 'utf8'))
      if (errors.length) violations.push(`${page}: invalid Vue template`)
      const template = descriptor.template?.content || ''
      if (/<(?:button|input|select|textarea)\b/.test(template)) violations.push(`${page}: use a shared control`)
      if (/<(?:RouterLink|a)\b[^>]*\bclass="button(?:\s|")/.test(template)) violations.push(`${page}: use UiButton as-child for navigation actions`)
      if (/window\.(?:confirm|alert)\s*\(/.test(descriptor.scriptSetup?.content || '')) violations.push(`${page}: use shared feedback`)
    }
    expect(violations).toEqual([])
  })

  it('keeps text readable and primitive appearance out of page styles', () => {
    const violations: string[] = []
    const visualProperty = /(?:^|;)\s*(?:height|min-height|padding(?:-[a-z]+)?|font(?:-[a-z]+)?|color|background(?:-[a-z]+)?|border(?:-[a-z]+)?|box-shadow)\s*:/
    for (const page of pages) {
      const { descriptor } = parse(readFileSync(join(root, 'views', page), 'utf8'))
      for (const style of descriptor.styles) {
        if (/font-size:\s*(?:[6-9]|10)px\b/.test(style.content)) violations.push(`${page}: text below 11px`)
        if (/font-weight:\s*(?:6[1-9]\d|[7-9]\d\d)\b/.test(style.content)) violations.push(`${page}: use 400, 500 or 600`)
        for (const rule of style.content.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
          if (/\.ui-(?:button|input|select|textarea)\b/.test(rule[1]) && visualProperty.test(rule[2])) violations.push(`${page}: primitive appearance in ${rule[1].trim()}`)
        }
      }
    }
    expect(violations).toEqual([])
  })
})
