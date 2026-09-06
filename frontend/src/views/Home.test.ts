import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterLink } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchPlanCatalogPage } from '../api/client'
import CommercePlanCard from '../components/CommercePlanCard.vue'
import Home from './Home.vue'

vi.mock('../api/client', () => ({ fetchPlanCatalogPage: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({
  siteProfile: { name: 'Fixture', homeTitle: 'Fixture title', description: 'Fixture description' },
  isAuthenticated: false, installation: { allow_registration: true },
}) }))
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
let wrapper: VueWrapper | undefined
beforeEach(() => vi.resetAllMocks())
afterEach(() => { wrapper?.unmount(); wrapper = undefined })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: Home }, { path: '/pricing', component: { template: '<div />' } },
    { path: '/register', component: { template: '<div />' } },
  ] })
  await router.push('/'); await router.isReady()
  wrapper = shallowMount(Home, { global: { plugins: [router], stubs: { RouterLink: false } } })
  return router
}

describe('homepage catalog delivery', () => {
  it('keeps the static introduction available while rendering real bounded recommendations', async () => {
    const pending = deferred<any>()
    vi.mocked(fetchPlanCatalogPage).mockReturnValue(pending.promise)
    const router = await render()
    const introduction = wrapper!.get('.storefront-hero__visual').text()
    expect(introduction).toBeTruthy()
    expect(wrapper!.find('.storefront-home-plans').exists()).toBe(false)
    expect(fetchPlanCatalogPage).toHaveBeenCalledWith({ offset: 0, limit: 3 }, expect.anything())
    pending.resolve({ items: [{ id: 42, name: 'Live offer', primary_sku: null, active_sku_count: 2 }], total: 1 })
    await flushPromises()
    expect(wrapper!.get('.storefront-hero__visual').text()).toBe(introduction)
    const card = wrapper!.getComponent(CommercePlanCard)
    expect(card.props('plan').id).toBe(42)
    expect(card.props('offerCount')).toBe(2)
    card.vm.$emit('select')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/pricing')
    expect(router.currentRoute.value.query.plan).toBe('42')
  })

  it('retains catalog navigation without invented offers when recommendations fail', async () => {
    vi.mocked(fetchPlanCatalogPage).mockRejectedValueOnce(new Error('offline'))
    await render(); await flushPromises()
    expect(wrapper!.find('.storefront-home-plans').exists()).toBe(false)
    expect(wrapper!.findAllComponents(CommercePlanCard)).toHaveLength(0)
    expect(wrapper!.findAllComponents(RouterLink).some(link => link.props('to') === '/pricing')).toBe(true)
    expect(wrapper!.get('.storefront-hero__visual').text()).toBeTruthy()
  })

  it('cancels the optional catalog read when the visitor leaves the page', async () => {
    const pending = deferred<any>()
    vi.mocked(fetchPlanCatalogPage).mockReturnValue(pending.promise)
    await render()
    const signal = vi.mocked(fetchPlanCatalogPage).mock.calls[0][1]?.signal
    wrapper!.unmount(); wrapper = undefined
    expect(signal?.aborted).toBe(true)
    pending.resolve({ items: [], total: 0 })
    await flushPromises()
    expect(fetchPlanCatalogPage).toHaveBeenCalledOnce()
  })
})
