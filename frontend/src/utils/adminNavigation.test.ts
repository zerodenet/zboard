import { describe, expect, it } from 'vitest'
import { routes } from '../router'
import { adminNavigation, resolveAdminNavigation } from './adminNavigation'

describe('admin navigation inventory', () => {
  const pages = adminNavigation.flatMap(domain => domain.sections.flatMap(section => section.pages))
  it('exposes every non-redirect admin page exactly once in six domains', () => {
    const admin = routes.find(route => route.path === '/admin')!
    const expected = admin.children!.filter(route => !route.redirect).map(route => `/admin/${route.path}`)
    expect(adminNavigation).toHaveLength(6)
    expect(pages.map(page => page.to).sort()).toEqual(expected.sort())
    expect(new Set(pages.map(page => page.to)).size).toBe(pages.length)
  })
  it.each(pages)('resolves $to from a direct URL with query and hash', page => {
    expect(resolveAdminNavigation(`${page.to}?page=2#details`)?.page).toBe(page)
  })
  it('selects only the most specific page for nested routes', () => {
    expect(resolveAdminNavigation('/admin/subscription-templates/rule-sets')?.page.label).toBe('规则集')
    expect(resolveAdminNavigation('/admin/subscription-templates/42')?.page.label).toBe('订阅模板')
    expect(resolveAdminNavigation('/admin/users-other')).toBeUndefined()
    expect(resolveAdminNavigation('/account')).toBeUndefined()
  })
  it('keeps maintenance in settings and announcements in operations', () => {
    expect(resolveAdminNavigation('/admin/maintenance')?.domain.id).toBe('settings')
    expect(resolveAdminNavigation('/admin/announcements')?.domain.id).toBe('operations')
  })
})
