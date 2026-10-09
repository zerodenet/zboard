import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AdminLayout from './AdminLayout.vue'
import PageHeader from '../components/PageHeader.vue'
import { adminNavigation, menuFixture } from '../test/menuFixtures'
import { fetchNavigation } from '../api/menus'
import { NAVIGATION_CHANGED } from '../utils/navigationEvents'

vi.mock('../api/menus', async () => { const { menuFixture } = await import('../test/menuFixtures'); return { fetchNavigation: vi.fn(async (surface: 'admin' | 'account' | 'public') => menuFixture(surface)) } })

const clearAuth = vi.hoisted(() => vi.fn())
vi.mock('../stores/app', () => ({ useAppStore: () => ({ siteName: 'zboard', user: { email: 'admin@example.test' }, loadMe: vi.fn(async () => undefined), clear: clearAuth }) }))
vi.mock('../components/TaskTray.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../api/system', () => ({ fetchAdminSystemInfo: vi.fn(async () => ({ release_version: '0.3.0', build_time: '2026-09-24T00:00:00Z' })) }))
vi.mock('../api/releaseUpdates', async importOriginal => ({ ...(await importOriginal<typeof import('../api/releaseUpdates')>()), fetchZBoardReleases: vi.fn(async () => [{ tag_name: 'v0.3.0', html_url: 'https://github.com/zerodenet/zboard/releases/tag/v0.3.0', prerelease: false, draft: false, published_at: '2026-09-24T00:00:00Z' }]) }))

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks(); clearAuth.mockClear(); vi.unstubAllGlobals(); document.body.style.overflow = '' })

async function setup(path: string, mobile = false) {
  let viewportListener: (() => void) | undefined
  const media = { matches: mobile, addEventListener: vi.fn((_event, listener) => { viewportListener = listener }), removeEventListener: vi.fn() }
  vi.stubGlobal('matchMedia', vi.fn(() => media))
  const pages = adminNavigation.flatMap(domain => domain.sections.flatMap(section => section.pages))
  const router = createRouter({ history: createMemoryHistory(), routes: [...pages, { to: '/', label: '首页' }, { to: '/login', label: '登录' }, { to: '/account', label: '个人中心' }, { to: '/admin/extensions/:pluginId/:pageId', label: '扩展' }].map(page => ({ path: page.to, component: { components: { PageHeader }, template: '<section class="standard-page"><PageHeader :title="String($route.meta.title)" /></section>' }, meta: { title: page.label } })) })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(AdminLayout, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router, resize: (matches: boolean) => { media.matches = matches; viewportListener?.() } }
}

describe('AdminLayout navigation', () => {
  it('keeps sibling page tabs outside the page header at narrow widths', async () => {
    const { wrapper, router } = await setup('/admin/nodes', true)
    const tabs = wrapper.get('.app-workspace > .mobile-section-navigation')
    expect(tabs.get('a[href="/admin/nodes"]').attributes('aria-current')).toBe('page')
    await tabs.get('a[href="/admin/protocols"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/protocols')
    expect(wrapper.get('.app-workspace > .mobile-section-navigation a[aria-current="page"]').attributes('href')).toBe('/admin/protocols')
  })

  it('offers other visible domain pages when the current menu section has one page', async () => {
    const menu = menuFixture('admin')
    menu.nodes = menu.nodes.map(node => node.parent_id === 'infrastructure.resources' && node.path !== '/admin/nodes'
      ? { ...node, parent_id: 'infrastructure.access' }
      : node)
    vi.mocked(fetchNavigation).mockResolvedValueOnce(menu)
    const { wrapper } = await setup('/admin/nodes', true)
    const tabs = wrapper.get('.app-workspace > .mobile-section-navigation')
    expect(tabs.get('a[href="/admin/nodes"]').attributes('aria-current')).toBe('page')
    expect(tabs.find('a[href="/admin/protocols"]').exists()).toBe(true)
  })

  it('shows an unavailable notice without mounting a directly opened hidden page', async () => {
    const value = menuFixture('admin')
    value.nodes = value.nodes.filter(node => node.path !== '/admin/nodes')
    value.page_available = false
    vi.mocked(fetchNavigation).mockResolvedValueOnce(value)
    const { wrapper, router } = await setup('/admin/nodes')
    expect(router.currentRoute.value.path).toBe('/admin/nodes')
    expect(wrapper.findAll('.domain-group.selected')).toHaveLength(0)
    expect(wrapper.get('.topbar-context').text()).toBe('管理控制台')
    expect(wrapper.find('.standard-page').exists()).toBe(false)
    expect(wrapper.get('main').text()).toContain('页面不可用')
    expect(fetchNavigation).toHaveBeenLastCalledWith('admin', expect.any(AbortSignal), '/admin/nodes')
  })

  it('removes the current page when refreshed navigation marks it hidden', async () => {
    const { wrapper } = await setup('/admin/nodes')
    expect(wrapper.find('.standard-page').exists()).toBe(true)
    const value = menuFixture('admin')
    value.nodes = value.nodes.filter(node => node.path !== '/admin/nodes')
    value.page_available = false
    vi.mocked(fetchNavigation).mockResolvedValueOnce(value)
    window.dispatchEvent(new Event(NAVIGATION_CHANGED))
    await flushPromises()
    expect(wrapper.find('.standard-page').exists()).toBe(false)
    expect(wrapper.get('main').text()).toContain('页面不可用')
  })

  it('keeps page content unmounted while a new path is checked and ignores late responses', async () => {
    const { wrapper, router } = await setup('/admin/nodes')
    let resolveOld!: (value: ReturnType<typeof menuFixture>) => void
    vi.mocked(fetchNavigation).mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    await router.push('/admin/protocols')
    expect(wrapper.find('.standard-page').exists()).toBe(false)
    expect(wrapper.get('main').text()).toContain('正在加载页面')
    const hidden = menuFixture('admin'); hidden.page_available = false
    vi.mocked(fetchNavigation).mockResolvedValueOnce(hidden)
    await router.push('/admin/users')
    await flushPromises()
    resolveOld(menuFixture('admin'))
    await flushPromises()
    expect(wrapper.get('main').text()).toContain('页面不可用')
    expect(wrapper.find('.standard-page').exists()).toBe(false)
  })

  it('keeps a visible sibling page mounted during tab navigation, then applies a revoked menu result', async () => {
    const { wrapper, router } = await setup('/admin/nodes')
    let resolveCheck!: (value: ReturnType<typeof menuFixture>) => void
    vi.mocked(fetchNavigation).mockImplementationOnce(() => new Promise(resolve => { resolveCheck = resolve }))
    await wrapper.get('.admin-page-navigation-link[href="/admin/protocols"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/protocols')
    expect(wrapper.find('.standard-page').exists()).toBe(true)
    expect(wrapper.get('main').text()).not.toContain('正在加载页面')
    const hidden = menuFixture('admin')
    hidden.nodes = hidden.nodes.filter(node => node.path !== '/admin/protocols')
    hidden.page_available = false
    resolveCheck(hidden)
    await flushPromises()
    expect(wrapper.find('.standard-page').exists()).toBe(false)
    expect(wrapper.get('main').text()).toContain('页面不可用')
  })

  it('shows a retry notice instead of page content when the initial check fails', async () => {
    vi.mocked(fetchNavigation).mockRejectedValueOnce(new Error('offline'))
    const { wrapper } = await setup('/admin/nodes')
    expect(wrapper.find('.standard-page').exists()).toBe(false)
    expect(wrapper.get('main').text()).toContain('暂时无法加载页面')
    await wrapper.get('main button').trigger('click')
    await flushPromises()
    expect(wrapper.find('.standard-page').exists()).toBe(true)
  })

  it('keeps loaded content during a connectivity failure and clears the warning on recovery', async () => {
    const { wrapper } = await setup('/admin/nodes')
    vi.mocked(fetchNavigation).mockRejectedValueOnce(new Error('offline'))
    window.dispatchEvent(new Event(NAVIGATION_CHANGED))
    await flushPromises()
    expect(wrapper.find('.standard-page').exists()).toBe(true)
    expect(wrapper.get('.navigation-notice').text()).toContain('当前内容已保留')
    expect(wrapper.get('main').text()).not.toContain('暂时无法加载页面')
    await wrapper.get('.navigation-notice button').trigger('click')
    await flushPromises()
    expect(wrapper.find('.navigation-notice').exists()).toBe(false)
  })

  it('revokes cached page approval on 403 without clearing the login', async () => {
    const { wrapper } = await setup('/admin/nodes')
    vi.mocked(fetchNavigation).mockRejectedValueOnce({ response: { status: 403 } })
    window.dispatchEvent(new Event(NAVIGATION_CHANGED))
    await flushPromises()
    expect(wrapper.find('.standard-page').exists()).toBe(false)
    expect(wrapper.get('main').text()).toContain('访问权限不足')
    expect(wrapper.get('main a').attributes('href')).toBe('/account')
    expect(clearAuth).not.toHaveBeenCalled()
    await wrapper.get('main button').trigger('click')
    await flushPromises()
    expect(wrapper.find('.standard-page').exists()).toBe(true)
  })

  it('offers login with the original location when navigation returns 401', async () => {
    vi.mocked(fetchNavigation).mockRejectedValueOnce({ response: { status: 401 } })
    const { wrapper } = await setup('/admin/nodes?page=3')
    expect(wrapper.get('main').text()).toContain('登录已过期')
    expect(wrapper.find('main button').exists()).toBe(false)
    expect(wrapper.get('main a').attributes('href')).toContain('redirect=/admin/nodes?page=3')
    expect(wrapper.get('main a').attributes('href')).toContain('reason=session-expired')
  })

  it('renders backend menu edits and plugin placement in sidebar, sibling tabs and search', async () => {
    const value = menuFixture('admin')
    value.nodes = value.nodes.filter(node => node.path !== '/admin/protocols')
    value.nodes.find(node => node.id === 'infrastructure')!.label = '服务接入'
    value.nodes.push({ ...value.nodes.find(node => node.path === '/admin/nodes')!, id: 'plugin:custom', label: '扩展控制台', path: '/admin/extensions/example/home', owner: 'plugin', plugin_id: 'example', page_id: 'home' })
    vi.mocked(fetchNavigation).mockResolvedValueOnce(value)
    const { wrapper } = await setup('/admin/nodes')
    expect(wrapper.get('.domain-group.selected > .domain-link').text()).toContain('服务接入')
    expect(wrapper.find('.admin-page-navigation-link[href="/admin/protocols"]').exists()).toBe(false)
    expect(wrapper.get('.admin-page-navigation-link[href="/admin/extensions/example/home"]').text()).toBe('扩展控制台')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true }))
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('input[aria-label="搜索管理页面"]')!
    input.value = '扩展控制台'; input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(document.querySelectorAll('.admin-quick-search-results a')).toHaveLength(1)
    expect(document.querySelector('.admin-quick-search-results a')?.getAttribute('href')).toBe('/admin/extensions/example/home')
  })

  it('searches admin pages from the topbar shortcut and follows a result', async () => {
    const { router } = await setup('/admin/dashboard')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true }))
    await flushPromises()
    const dialog = document.querySelector<HTMLElement>('.app-dialog')
    expect(dialog?.parentElement).toBe(document.body)
    expect(dialog?.querySelector('.app-dialog-body')).not.toBeNull()
    const search = document.querySelector<HTMLInputElement>('input[aria-label="搜索管理页面"]')
    expect(search).not.toBeNull()
    search!.value = '工单'
    search!.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    const results = document.querySelectorAll<HTMLAnchorElement>('.admin-quick-search-results a')
    expect(results).toHaveLength(1)
    expect(results[0]?.getAttribute('href')).toBe('/admin/tickets')
    results[0]?.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/tickets')
  })

  it('shows just the URL-owned domain and one active page on a deep link', async () => {
    const { wrapper } = await setup('/admin/subscription-templates/rule-sets?search=test')
    expect(wrapper.findAll('.domain-link')).toHaveLength(5)
    expect(wrapper.get('.domain-group.selected > .domain-link').attributes('aria-label')).toBe('订阅系统')
    expect(wrapper.findAll('.page-link[aria-current="page"]')).toHaveLength(1)
    expect(wrapper.get('.page-link.selected').text()).toBe('配置交付')
    expect(wrapper.findAll('.page-link')).toHaveLength(3)
    expect(wrapper.get('.admin-page-navigation-link[aria-current="page"]').text()).toBe('规则集')
    expect(wrapper.get('.topbar-context').text()).toBe('订阅系统')
    expect(wrapper.get('.admin-build-info').text()).toContain('版本 0.3.0')
    expect(wrapper.get('.admin-build-info').text()).toContain('构建时间')
    expect(wrapper.find('.admin-account').exists()).toBe(false)
  })

  it('keeps site, account and logout actions in the top-right account menu', async () => {
    const { wrapper, router } = await setup('/admin/users')
    await wrapper.get('.topbar-account-trigger').trigger('click')
    await flushPromises()
    const menu = document.body.querySelector<HTMLElement>('[role="menu"]')
    expect(menu?.querySelector<HTMLAnchorElement>('a[href="/"]')?.textContent).toContain('查看站点')
    const account = menu?.querySelector<HTMLAnchorElement>('a[href="/account"]')
    expect(account?.textContent).toContain('个人中心')
    expect(menu?.textContent).toContain('退出登录')
    account!.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/account')
  })

  it('signs out from the account menu', async () => {
    const { wrapper, router } = await setup('/admin/users')
    await wrapper.get('.topbar-account-trigger').trigger('click')
    await flushPromises()
    const logout = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(item => item.textContent?.includes('退出登录'))
    expect(logout).toBeDefined()
    logout!.click()
    await flushPromises()
    expect(clearAuth).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('expands the overview and four core modules without navigation, then follows sections and browser history', async () => {
    const { wrapper, router } = await setup('/admin/users?page=3')
    await wrapper.get('.domain-link[aria-label="用户系统"]').trigger('click')
    expect(router.currentRoute.value.fullPath).toBe('/admin/users?page=3')
    expect(wrapper.get('.domain-link[aria-label="用户系统"]').attributes('aria-expanded')).toBe('false')
    for (const domain of adminNavigation) {
      const previousRoute = router.currentRoute.value.fullPath
      await wrapper.get(`.domain-link[aria-label="${domain.label}"]`).trigger('click')
      await flushPromises()
      expect(wrapper.get(`.domain-link[aria-label="${domain.label}"]`).attributes('aria-expanded')).toBe('true')
      expect(router.currentRoute.value.fullPath).toBe(previousRoute)
      await wrapper.get(`.page-link[href="${domain.sections[0].pages[0].to}"]`).trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe(domain.sections[0].pages[0].to)
      expect(wrapper.get('.domain-group.selected > .domain-link').attributes('aria-label')).toBe(domain.label)
    }
    router.back()
    await flushPromises()
    expect(wrapper.get('.domain-group.selected > .domain-link').attributes('aria-label')).toBe('节点系统')
    router.forward()
    await flushPromises()
    expect(wrapper.get('.domain-group.selected > .domain-link').attributes('aria-label')).toBe('服务系统')
  })

  it('keeps the mobile drawer open for domain selection, then closes for the page and restores focus', async () => {
    const { wrapper, router } = await setup('/admin/dashboard', true)
    expect(wrapper.get('aside').attributes('inert')).toBeDefined()
    const toggle = wrapper.get<HTMLButtonElement>('.menu-button')
    toggle.element.focus()
    await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.get('aside').attributes('aria-modal')).toBe('true')
    expect(wrapper.get('.app-workspace').attributes('inert')).toBeDefined()
    expect(document.body.style.overflow).toBe('hidden')
    expect(document.activeElement).toBe(wrapper.get('.sidebar-close').element)
    await wrapper.get('.domain-link[aria-label="节点系统"]').trigger('click')
    await flushPromises()
    expect(wrapper.classes()).toContain('nav-open')
    await wrapper.get('.page-link[href="/admin/nodes"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/nodes')
    expect(wrapper.classes()).not.toContain('nav-open')
    expect(document.activeElement).toBe(toggle.element)
    await wrapper.get('.admin-page-navigation-link[href="/admin/protocols"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/protocols')
    expect(document.body.style.overflow).toBe('')
  })

  it('closes on Escape, scrim, browser history and desktop resize without leaving scroll locked', async () => {
    const { wrapper, router, resize } = await setup('/admin/maintenance', true)
    await wrapper.get('.menu-button').trigger('click')
    await flushPromises()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.classes()).not.toContain('nav-open')
    await wrapper.get('.menu-button').trigger('click')
    await wrapper.get('.nav-scrim').trigger('click')
    expect(wrapper.classes()).not.toContain('nav-open')
    await wrapper.get('.menu-button').trigger('click')
    await router.push('/admin/settings/site')
    await flushPromises()
    expect(wrapper.classes()).not.toContain('nav-open')
    await wrapper.get('.menu-button').trigger('click')
    resize(false)
    await flushPromises()
    expect(wrapper.get('aside').attributes('inert')).toBeUndefined()
    expect(wrapper.get('.app-workspace').attributes('inert')).toBeUndefined()
    expect(document.body.style.overflow).toBe('')
  })

  it('wraps keyboard focus within the mobile drawer and releases it on unmount', async () => {
    const { wrapper } = await setup('/admin/maintenance', true)
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList)
    document.body.style.overflow = 'scroll'
    await wrapper.get('.menu-button').trigger('click')
    await flushPromises()
    const first = wrapper.get<HTMLAnchorElement>('.admin-brand-home').element
    const last = wrapper.findAll<HTMLElement>('.app-sidebar a[href]').at(-1)!.element
    last.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', cancelable: true }))
    expect(document.activeElement).toBe(first)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, cancelable: true }))
    expect(document.activeElement).toBe(last)
    wrapper.unmount()
    expect(document.body.style.overflow).toBe('scroll')
  })
})
