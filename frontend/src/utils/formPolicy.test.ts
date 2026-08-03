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

function relativeSource(path: string) {
  return relative(sourceRoot, path).replace(/\\/g, '/')
}

describe('form feedback policy', () => {
  it('routes validation through application feedback instead of browser-native bubbles', () => {
    const violations = vueFiles(sourceRoot).flatMap(path => {
      const forms = readFileSync(path, 'utf8').match(/<form\b[^>]*>/g) || []
      return forms
        .filter(form => !/\bnovalidate\b/.test(form))
        .map(form => `${relative(sourceRoot, path)}: ${form}`)
    })

    expect(violations).toEqual([])
  })

  it('keeps every write form on the shared field, validation and API-error contract', () => {
    const owners = vueFiles(sourceRoot)
      .map(path => ({ path, source: readFileSync(path, 'utf8') }))
      .filter(item => /<form\b/.test(item.source))
    const formCount = owners.reduce((count, item) => count + (item.source.match(/<form\b/g) || []).length, 0)
    const ownerViolations = owners.flatMap(({ path, source }) => {
      const missing = [
        !source.includes('<FormField') && 'FormField',
        !source.includes('useFormErrors') && 'useFormErrors',
        !source.includes('applyValidation') && 'applyValidation',
        !source.includes('applyApiError') && 'applyApiError',
        !source.includes('watch(') && 'field error clearing',
      ].filter(Boolean)
      return missing.map(contract => `${relativeSource(path)}: ${contract}`)
    })
    const formViolations = owners.flatMap(({ path, source }) =>
      (source.match(/<form\b[\s\S]*?<\/form>/g) || []).flatMap((form, index) => [
        !form.includes('<PageAlert') && `${relativeSource(path)} form ${index + 1}: PageAlert`,
        !form.includes('formError.value') && `${relativeSource(path)} form ${index + 1}: formError summary`,
      ].filter((violation): violation is string => Boolean(violation))),
    )

    expect(formCount).toBe(23)
    expect([...ownerViolations, ...formViolations]).toEqual([])
  })

  it('associates setup wizard actions with their forms for keyboard submission', () => {
    const setup = readFileSync(join(sourceRoot, 'views', 'Setup.vue'), 'utf8')
    expect(setup).toContain('id="setup-site-form"')
    expect(setup).toContain('form="setup-site-form" type="submit"')
    expect(setup).toContain('id="setup-admin-form"')
    expect(setup).toContain('form="setup-admin-form" type="submit"')
  })

  it('protects all persistent business forms from route changes and browser unload', () => {
    const expectedGuardedOwners = [
      'components/TicketCenter.vue',
      'views/Certificates.vue',
      'views/NodeGroups.vue',
      'views/Nodes.vue',
      'views/Plans.vue',
      'views/Protocols.vue',
      'views/Settings.vue',
      'views/Setup.vue',
      'views/SubscriptionRuleSets.vue',
      'views/SubscriptionTemplates.vue',
      'views/Tasks.vue',
      'views/Users.vue',
    ]
    const actualGuardedOwners = vueFiles(sourceRoot)
      .filter(path => readFileSync(path, 'utf8').includes('useUnsavedChangesGuard('))
      .map(relativeSource)
      .sort()

    expect(actualGuardedOwners).toEqual(expectedGuardedOwners)
  })
})
