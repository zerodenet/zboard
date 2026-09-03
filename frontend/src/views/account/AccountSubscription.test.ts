import PrimeVue from 'primevue/config'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAccountProtocolLoads, fetchAccountSubscriptionsPage, fetchActiveSubscriptionTemplatesPage } from '../../api/client'
import { fetchSubscriptionAccess, revokeSubscriptionAccess, rotateSubscriptionAccess, type SubscriptionAccess } from '../../api/subscriptionAccess'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
import AccountSubscription from './AccountSubscription.vue'

vi.mock('../../api/client', () => ({
  fetchAccountSubscriptionsPage: vi.fn(),
  fetchActiveSubscriptionTemplatesPage: vi.fn(),
  fetchAccountProtocolLoads: vi.fn(),
}))
vi.mock('../../api/subscriptionAccess', () => ({
  fetchSubscriptionAccess: vi.fn(),
  rotateSubscriptionAccess: vi.fn(),
  revokeSubscriptionAccess: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

const subscriptions = [1, 2].map(id => ({
  id, status: 'active', plan_name: `Plan ${id}`, sku_name: 'Monthly',
  plan_id: id, plan_sku_id: id, flow_used: 10, flow_total: 100,
  end_at: '2027-01-01T00:00:00Z',
}))
function page(items: unknown[]) {
  return { items, total: items.length, page: { offset: 0, limit: 25, total: items.length }, aggregates: {}, facets: {} } as any
}
function access(id: number): SubscriptionAccess {
  return { configured: true, subscription_id: id, token_prefix: `fixture-${id}`, subscription_url: `/fixture-access/${id}` }
}

describe('account subscription independent loading and access identity', () => {
  let wrapper: VueWrapper | undefined
  let router: Router

  beforeEach(() => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page(subscriptions))
    vi.mocked(fetchActiveSubscriptionTemplatesPage).mockResolvedValue(page([]))
    vi.mocked(fetchAccountProtocolLoads).mockResolvedValue({ sampled_at: '', activity_window_seconds: 120, items: [] })
    vi.mocked(fetchSubscriptionAccess).mockImplementation(async id => access(id))
    vi.mocked(rotateSubscriptionAccess).mockImplementation(async id => access(id))
    vi.mocked(revokeSubscriptionAccess).mockResolvedValue(undefined as any)
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })

  async function open(path = '/account/subscription') {
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/account/subscription', component: { template: '<div />' } },
      { path: '/account/plans', component: { template: '<div />' } },
    ] })
    await router.push(path)
    await router.isReady()
    wrapper = mount(AccountSubscription, { global: { plugins: [router, PrimeVue], stubs: { ConfirmDialog: true } } })
    await flushPromises()
    return wrapper
  }
  function button(text: string) {
    const match = wrapper!.findAll('button').find(item => item.text().includes(text))
    expect(match, `button ${text}`).toBeDefined()
    return match!
  }

  it('resolves a deep-linked subscription outside both the preview and current list page', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockImplementation(async params => params?.subscriptionId
      ? page([{ ...subscriptions[0], id: 201, plan_name: 'Old subscription' }])
      : page(subscriptions))
    await open('/account/subscription?subscription=201')
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith(
      { subscriptionId: 201, status: 'active', offset: 0, limit: 1 }, expect.anything(),
    )
    expect(fetchSubscriptionAccess).toHaveBeenCalledWith(201, expect.anything())
    expect(wrapper!.text()).toContain('Old subscription 的链接已启用')
    expect(router.currentRoute.value.query.subscription).toBe('201')
  })

  it('does not fall back to another subscription when an explicit target is unavailable', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockImplementation(async params => params?.subscriptionId ? page([]) : page(subscriptions))
    await open('/account/subscription?subscription=201')
    expect(fetchSubscriptionAccess).not.toHaveBeenCalled()
    expect(wrapper!.text()).toContain('所选订阅不存在或已失效')
    expect(router.currentRoute.value.query.subscription).toBe('201')
  })

  it('loads the link without waiting for the list, templates or protocol loads', async () => {
    const slow = deferred<any>()
    vi.mocked(fetchAccountSubscriptionsPage).mockImplementation(async params => params?.status === 'active' ? page(subscriptions) : slow.promise)
    vi.mocked(fetchActiveSubscriptionTemplatesPage).mockReturnValue(slow.promise)
    vi.mocked(fetchAccountProtocolLoads).mockReturnValue(slow.promise)
    await open()
    expect(fetchSubscriptionAccess).toHaveBeenCalledOnce()
    expect(wrapper!.text()).toContain('Plan 1 的链接已启用')
    expect(button('复制链接').exists()).toBe(true)
  })

  it('keeps links usable and distinguishes failed optional sections from empty data', async () => {
    vi.mocked(fetchAccountProtocolLoads).mockRejectedValue(new Error('fixture failure'))
    vi.mocked(fetchActiveSubscriptionTemplatesPage).mockRejectedValue(new Error('fixture failure'))
    await open()
    expect(wrapper!.text()).toContain('Plan 1 的链接已启用')
    expect(wrapper!.text()).toContain('协议实时负载加载失败')
    expect(wrapper!.text()).not.toContain('暂无可用协议')
    expect(button('复制链接').exists()).toBe(true)
    const reads = vi.mocked(fetchSubscriptionAccess).mock.calls.length
    vi.mocked(fetchAccountProtocolLoads).mockResolvedValue({ sampled_at: '', activity_window_seconds: 120, items: [] })
    await button('重试负载').trigger('click')
    await flushPromises()
    expect(fetchSubscriptionAccess).toHaveBeenCalledTimes(reads)
    expect(wrapper!.text()).toContain('暂无可用协议')
  })

  it('immediately hides the previous link and rejects a late response after selection changes', async () => {
    const old = deferred<SubscriptionAccess>(), current = deferred<SubscriptionAccess>()
    vi.mocked(fetchSubscriptionAccess).mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    await open()
    expect(wrapper!.text()).toContain('正在读取链接状态')
    expect(button('生成此链接').attributes('disabled')).toBeDefined()
    await button('管理链接').trigger('click')
    await flushPromises()
    expect(vi.mocked(fetchSubscriptionAccess).mock.calls[0][1]?.signal?.aborted).toBe(true)
    current.resolve(access(2))
    await flushPromises()
    old.resolve(access(1))
    await flushPromises()
    expect(wrapper!.text()).toContain('Plan 2 的链接已启用')
    expect(wrapper!.text()).not.toContain('fixture-1')
    expect(wrapper!.text()).toContain('fixture-2')
  })

  it('clears a loaded link before the new selection request completes', async () => {
    await open()
    const next = deferred<SubscriptionAccess>()
    vi.mocked(fetchSubscriptionAccess).mockReturnValue(next.promise)
    await button('管理链接').trigger('click')
    expect(wrapper!.text()).not.toContain('fixture-1')
    expect(wrapper!.text()).not.toContain('此订阅尚未生成可用链接')
    expect(wrapper!.findAll('button').some(item => item.text().includes('复制链接'))).toBe(false)
  })

  it('shows a failed access read as unknown, not unconfigured, and allows retry', async () => {
    vi.mocked(fetchSubscriptionAccess).mockRejectedValueOnce(new Error('fixture timeout'))
    await open()
    expect(wrapper!.text()).toContain('订阅链接读取失败')
    expect(wrapper!.text()).not.toContain('此订阅尚未生成可用链接')
    expect(button('生成此链接').attributes('disabled')).toBeDefined()
    await button('重试链接').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('Plan 1 的链接已启用')
  })

  it('rejects a response whose subscription id does not match the request', async () => {
    vi.mocked(fetchSubscriptionAccess).mockResolvedValue(access(2))
    await open()
    expect(wrapper!.text()).toContain('订阅链接读取失败')
    expect(wrapper!.text()).not.toContain('fixture-2')
  })

  it.each(['rotate', 'revoke'] as const)('does not display a late %s result after navigating to another subscription', async action => {
    const mutation = deferred<any>()
    const mutate = action === 'rotate' ? rotateSubscriptionAccess : revokeSubscriptionAccess
    vi.mocked(mutate).mockReturnValue(mutation.promise)
    await open()
    await button(action === 'rotate' ? '轮换此链接' : '吊销此链接').trigger('click')
    wrapper!.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(mutate).toHaveBeenCalledWith(1)
    await router.push({ path: '/account/subscription', query: { subscription: '2' } })
    await flushPromises()
    mutation.resolve(access(1))
    await flushPromises()
    expect(wrapper!.text()).toContain('Plan 2 的链接已启用')
    expect(wrapper!.text()).not.toContain('fixture-1')
    expect(wrapper!.text()).not.toContain('订阅 #1 的链接已轮换')
  })

  it('requires a fresh read after a mutation fails instead of offering a blind retry', async () => {
    vi.mocked(rotateSubscriptionAccess).mockRejectedValueOnce(new Error('fixture timeout'))
    await open()
    await button('轮换此链接').trigger('click')
    wrapper!.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper!.findComponent(ConfirmDialog).props('open')).toBe(false)
    expect(wrapper!.text()).toContain('请重新读取状态后再试')
    expect(button('生成此链接').attributes('disabled')).toBeDefined()
    await button('重试链接').trigger('click')
    await flushPromises()
    expect(button('轮换此链接').attributes('disabled')).toBeUndefined()
    expect(rotateSubscriptionAccess).toHaveBeenCalledOnce()
  })

  it('reloads settled state when returning to the original subscription during a mutation', async () => {
    const mutation = deferred<SubscriptionAccess>()
    vi.mocked(rotateSubscriptionAccess).mockReturnValue(mutation.promise)
    await open()
    await button('轮换此链接').trigger('click')
    wrapper!.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    await router.push({ path: '/account/subscription', query: { subscription: '2' } })
    await flushPromises()
    await router.push({ path: '/account/subscription', query: { subscription: '1' } })
    await flushPromises()
    expect(fetchSubscriptionAccess).toHaveBeenCalledTimes(2)
    mutation.resolve({ ...access(1), token_prefix: 'outdated-mutation-view' })
    await flushPromises()
    expect(fetchSubscriptionAccess).toHaveBeenCalledTimes(3)
    expect(wrapper!.text()).toContain('Plan 1 的链接已启用')
    expect(wrapper!.text()).not.toContain('outdated-mutation-view')
    expect(wrapper!.text()).not.toContain('正在读取链接状态')
  })

  it('aborts pending reads on unmount without fetching a link or navigating later', async () => {
    const pending = deferred<any>()
    vi.mocked(fetchAccountSubscriptionsPage).mockReturnValue(pending.promise)
    await open()
    const replace = vi.spyOn(router, 'replace')
    const signals = vi.mocked(fetchAccountSubscriptionsPage).mock.calls.map(([, options]) => options?.signal)
    wrapper!.unmount(); wrapper = undefined
    expect(signals.every(signal => signal?.aborted)).toBe(true)
    pending.resolve(page(subscriptions))
    await flushPromises()
    expect(fetchSubscriptionAccess).not.toHaveBeenCalled()
    expect(replace).not.toHaveBeenCalled()
  })
})
