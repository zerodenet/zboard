import { describe, expect, it } from 'vitest'
import { routes } from '../router'
import { adminNavigation, resolveAdminNavigation } from '../test/menuFixtures'

describe('admin navigation inventory', () => {
  const pages = adminNavigation.flatMap(domain => domain.sections.flatMap(section => section.pages))
  it('exposes every static non-redirect admin page exactly once in the overview and four core modules', () => {
    const admin = routes.find(route => route.path === '/admin')!
    const expected = admin.children!.filter(route => !route.redirect && !route.path.includes(':') && !route.meta?.pluginPage).map(route => `/admin/${route.path}`)
    expect(adminNavigation.map(domain => domain.label)).toEqual(['概览', '用户系统', '订阅系统', '节点系统', '服务系统'])
    expect(pages.map(page => page.to).sort()).toEqual(expected.sort())
    expect(new Set(pages.map(page => page.to)).size).toBe(pages.length)
  })
  it.each(pages)('resolves $to from a direct URL with query and hash', page => {
    expect(resolveAdminNavigation(`${page.to}?page=2#details`)?.page).toBe(page)
  })
  it('selects only the most specific page for nested routes', () => {
    expect(resolveAdminNavigation('/admin/subscription-templates/rule-sets')?.page.label).toBe('规则集')
    expect(resolveAdminNavigation('/admin/subscription-templates/42')?.page.label).toBe('订阅模板')
    expect(resolveAdminNavigation('/admin/plugins/zboard.oauth')?.page.label).toBe('插件管理')
    expect(resolveAdminNavigation('/admin/plugins/zboard.oauth/configuration')?.page.label).toBe('插件管理')
    expect(resolveAdminNavigation('/admin/users-other')).toBeUndefined()
    expect(resolveAdminNavigation('/account')).toBeUndefined()
  })
  it('groups user content, subscriptions and service operations under their owning modules', () => {
    expect(resolveAdminNavigation('/admin/extensions/example.welcome/home')).toBeUndefined()
    expect(resolveAdminNavigation('/admin/maintenance')?.domain.id).toBe('settings')
    expect(resolveAdminNavigation('/admin/announcements')?.domain.id).toBe('customers')
    expect(resolveAdminNavigation('/admin/announcements')?.section.label).toBe('内容管理')
    expect(resolveAdminNavigation('/admin/subscriptions')?.domain.id).toBe('commerce')
    for (const path of ['/admin/tasks', '/admin/runtime-jobs']) {
      expect(resolveAdminNavigation(path)?.domain.id).toBe('settings')
      expect(resolveAdminNavigation(path)?.section.label).toBe('任务队列')
    }
    expect(resolveAdminNavigation('/admin/operation-logs')?.domain.id).toBe('settings')
    expect(resolveAdminNavigation('/admin/settings/email')?.section.label).toBe('系统配置')
    expect(resolveAdminNavigation('/admin/plugins/zboard.oauth')?.section.label).toBe('扩展中心')
  })
})
