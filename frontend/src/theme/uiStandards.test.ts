import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { parse } from 'vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

const root = join(import.meta.dirname, '..')
// Derive coverage from real admin routes, so a newly added page is checked too.
const adminRoutes = readFileSync(join(root, 'router/index.ts'), 'utf8').split("path: '/admin',")[1]
const adminPages = [...new Set(Array.from(adminRoutes.matchAll(/import\('\.\.\/views\/([^']+\.vue)'\)/g), match => match[1]))]

const accountPages = [...new Set(Array.from(readFileSync(join(root, 'router/index.ts'), 'utf8').matchAll(/import\('\.\.\/views\/(account\/[^']+\.vue)'\)/g), match => match[1]))]
const pages = [...new Set([...adminPages, ...accountPages])]

describe('admin and account UI standard v2', () => {
  it('keeps every routed console template valid and uses shared form controls', () => {
    const violations: string[] = []
    for (const page of pages) {
      const { descriptor, errors } = parse(readFileSync(join(root, 'views', page), 'utf8'))
      if (errors.length) violations.push(`${page}: invalid Vue template`)
      const template = descriptor.template?.content || ''
      if ((page.startsWith('account/') ? /<(?:input|select|textarea)\b/ : /<(?:button|input|select|textarea)\b/).test(template)) violations.push(`${page}: use a shared control`)
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

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => entry.isDirectory()
    ? vueFiles(join(directory, entry.name)) : entry.name.endsWith('.vue') ? [join(directory, entry.name)] : [])
}
it('keeps status cells free of action controls across admin, account and shared tables', () => {
  const violations: string[] = []
  for (const file of [...vueFiles(join(root, 'views')), ...vueFiles(join(root, 'components'))]) {
    const { descriptor } = parse(readFileSync(file, 'utf8'))
    for (const cell of (descriptor.template?.content || '').matchAll(/<td\b[^>]*>([\s\S]*?)<\/td>/g)) {
      if (/<StatusBadge\b/.test(cell[1]) && /<(?:UiButton|RowActions|RowActionMenu)\b/.test(cell[1])) violations.push(file.replace(root, ''))
    }
  }
  expect(violations).toEqual([])
})

it('avoids native titles that only repeat fully rendered text', () => {
  const violations: string[] = []
  for (const file of [...vueFiles(join(root, 'views')), ...vueFiles(join(root, 'components')), ...vueFiles(join(root, 'layouts'))]) {
    const { descriptor } = parse(readFileSync(file, 'utf8'))
    for (const element of (descriptor.template?.content || '').matchAll(/<(?:span|small|strong|p|a|RouterLink)\b([^>]*)>\s*{{\s*([^}]+?)\s*}}/g)) {
      const title = element[1].match(/:title="([^"]+)"/)
      if (title?.[1].trim() === element[2].trim()) violations.push(file.replace(root, ''))
    }
  }
  expect(violations).toEqual([])
})

it('keeps the shared console typography within the same weight scale', () => {
  const violations: string[] = []
  for (const file of vueFiles(join(root, 'components')).filter(file => !file.includes('/components/ui/'))) {
    const { descriptor } = parse(readFileSync(file, 'utf8'))
    if (descriptor.styles.some(style => /font-weight:\s*(?:6[1-9]\d|[7-9]\d\d)\b/.test(style.content))) violations.push(file.replace(root, ''))
  }
  for (const file of ['styles.css', 'styles/account.css', 'styles/commerce.css', 'styles/commerce-catalog.css', 'styles/commerce-storefront.css']) {
    if (/font-weight:\s*(?:6[1-9]\d|[7-9]\d\d)\b/.test(readFileSync(join(root, file), 'utf8'))) violations.push(file)
  }
  expect(violations).toEqual([])
})
