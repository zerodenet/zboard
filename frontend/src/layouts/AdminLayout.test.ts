import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AdminLayout from './AdminLayout.vue'
import { adminNavigation } from '../utils/adminNavigation'

vi.mock('../stores/app', () => ({ useAppStore: () => ({ siteName: 'zboard', user: { email: 'admin@example.test' }, loadMe: vi.fn(), clear: vi.fn() }) }))
vi.mock('../components/TaskTray.vue', () => ({ default: { template: '<div />' } }))

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.style.overflow = '' })

async function setup(path: string, mobile = false) {
  let viewportListener: (() => void) | undefined
  const media = { matches: mobile, addEventListener: vi.fn((_event, listener) => { viewportListener = listener }), removeEventListener: vi.fn() }
  vi.stubGlobal('matchMedia', vi.fn(() => media))
  const pages = adminNavigation.flatMap(domain => domain.sections.flatMap(section => section.pages))
  const router = createRouter({ history: createMemoryHistory(), routes: [...pages, { to: '/', label: '首页' }, { to: '/account', label: '个人中心' }].map(page => ({ path: page.to, component: { template: '<p>Page fixture</p>' }, meta: { title: page.label } })) })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(AdminLayout, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router, resize: (matches: boolean) => { media.matches = matches; viewportListener?.() } }
}

describe('AdminLayout navigation', () => {
  it('shows just the URL-owned domain and one active page on a deep link', async () => {
    const { wrapper } = await setup('/admin/subscription-templates/rule-sets?search=test')
    expect(wrapper.findAll('.domain-link')).toHaveLength(6)
    expect(wrapper.get('.domain-link[aria-current="true"]').attributes('aria-label')).toBe('商品与订单')
    expect(wrapper.findAll('.page-link[aria-current="page"]')).toHaveLength(1)
    expect(wrapper.get('.page-link.selected').text()).toBe('规则集')
    expect(wrapper.findAll('.page-link')).toHaveLength(4)
    expect(wrapper.get('.topbar-context').text()).toBe('商品与订单')
    expect(wrapper.find('.build-version').exists()).toBe(false)
  })

  it('navigates all six domains, follows history, and does not reset the current domain query', async () => {
    const { wrapper, router } = await setup('/admin/users?page=3')
    await wrapper.get('.domain-link[aria-label="用户与订阅"]').trigger('click')
    expect(router.currentRoute.value.fullPath).toBe('/admin/users?page=3')
    for (const domain of adminNavigation) {
      await wrapper.get(`.domain-link[aria-label="${domain.label}"]`).trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe(domain.sections[0].pages[0].to)
      expect(wrapper.get('.domain-heading h2').text()).toBe(domain.label)
    }
    router.back()
    await flushPromises()
    expect(wrapper.get('.domain-heading h2').text()).toBe('运营')
    router.forward()
    await flushPromises()
    expect(wrapper.get('.domain-heading h2').text()).toBe('设置')
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
    await wrapper.get('.domain-link[aria-label="节点与协议"]').trigger('click')
    await flushPromises()
    expect(wrapper.classes()).toContain('nav-open')
    await wrapper.get('.page-link[href="/admin/protocols"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/protocols')
    expect(wrapper.classes()).not.toContain('nav-open')
    expect(document.activeElement).toBe(toggle.element)
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
    const last = wrapper.get<HTMLButtonElement>('[aria-label="退出登录"]').element
    last.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', cancelable: true }))
    expect(document.activeElement).toBe(first)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, cancelable: true }))
    expect(document.activeElement).toBe(last)
    wrapper.unmount()
    expect(document.body.style.overflow).toBe('scroll')
  })
})
