import { reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createWalletTopUp, fetchWallet } from '../../api/wallet'
import AccountWallet from './AccountWallet.vue'
const state = vi.hoisted(() => ({ user: { id: 1 }, publicConfigs: [] as { config_key: string; value: string }[] }))
vi.mock('../../stores/app', () => ({ useAppStore: () => reactive(state) }))
vi.mock('../../api/wallet', () => ({ createWalletTopUp: vi.fn(), fetchWallet: vi.fn() }))
function page(balance = '12000') { return { currency: 'CNY', balance_minor: balance, available_minor: balance, reserved_minor: '0', entries: [], has_more: false } }
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/account/wallet', component: AccountWallet }, { path: '/account/orders', component: { template: '<div />' } }, { path: '/account/orders/:id', component: { template: '<div />' } }, { path: '/account/tickets', component: { template: '<div />' } }] })
  await router.push('/account/wallet'); await router.isReady()
  const wrapper = mount(AccountWallet, { global: { plugins: [router] } }); await flushPromises()
  return { wrapper, router }
}
beforeEach(() => { localStorage.clear(); vi.clearAllMocks(); state.publicConfigs = []; reactive(state).user.id = 1; vi.mocked(fetchWallet).mockResolvedValue(page()); vi.mocked(createWalletTopUp).mockResolvedValue({ id: 91 }) })
describe('wallet account and top-up intent', () => {
  it('links the actual wallet entry to an unsubmitted ticket without changing money or creating orders', async () => {
    vi.mocked(fetchWallet).mockResolvedValue({ ...page(), entries: [{ id: 8, kind: 'credit', balance_delta: '100', reserved_delta: '0', balance_after: '12000', reserved_after: '0', source_order_id: 27, reason: '余额核对', created_at: '2026-10-20T00:00:00Z' }] })
    const { wrapper, router } = await open()
    const support = wrapper.get('a[aria-label="就此流水提交工单"]')
    expect(support.attributes('href')).toBe('/account/tickets?new=1&wallet_entry_id=8')
    expect(router.currentRoute.value.path).toBe('/account/wallet')
    expect(createWalletTopUp).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('recovers a lost creation response using the same amount and idempotency key', async () => {
    vi.mocked(createWalletTopUp).mockRejectedValueOnce(new Error('lost response'))
    const { wrapper, router } = await open()
    expect(wrapper.get('[aria-label="CNY 余额"]').text()).toContain('120.00')
    await wrapper.get('input[placeholder="例如 10.00"]').setValue('0.29')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const first = vi.mocked(createWalletTopUp).mock.calls[0]![0]
    expect(first).toEqual({ amount_minor: 29, currency: 'CNY', idempotency_key: expect.any(String) })
    expect(wrapper.get('input[placeholder="例如 10.00"]').attributes('disabled')).toBeDefined()
    expect(router.currentRoute.value.path).toBe('/account/wallet')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(vi.mocked(createWalletTopUp).mock.calls[1]![0]).toEqual(first)
    expect(router.currentRoute.value.path).toBe('/account/orders/91')
    wrapper.unmount()
  })
  it('clears old account data and drafts when the authenticated account changes', async () => {
    const { wrapper } = await open()
    await wrapper.get('input[placeholder="例如 10.00"]').setValue('25.00')
    vi.mocked(fetchWallet).mockResolvedValue(page('0'))
    reactive(state).user.id = 2; await flushPromises()
    expect(wrapper.get('[aria-label="CNY 余额"]').text()).toContain('-')
    expect(wrapper.text()).not.toContain('CNY 120.00')
    expect((wrapper.get('input[placeholder="例如 10.00"]').element as HTMLInputElement).value).toBe('')
    expect(createWalletTopUp).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

it('keeps a zero ledger transaction explicit while zero balances stay quiet', async () => {
  vi.mocked(fetchWallet).mockResolvedValue({ ...page('0'), entries: [{ id: 8, kind: 'release', balance_delta: '0', reserved_delta: '-100', balance_after: '0', reserved_after: '0', reason: '释放预留', created_at: '2026-10-20T00:00:00Z' }] })
  const { wrapper } = await open()
  expect(wrapper.get('td[aria-label="CNY 0.00"]').text()).toBe('0.00')
  expect(wrapper.get('[aria-label="CNY 余额"]').text()).toContain('-')
  expect(createWalletTopUp).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('uses the configured currency for a fresh top-up', async () => {
  state.publicConfigs = [{ config_key: 'default_currency', value: 'USD' }]
  vi.mocked(fetchWallet).mockResolvedValue({ ...page('0'), currency: 'USD' })
  const { wrapper } = await open()
  expect(fetchWallet).toHaveBeenCalledWith('USD', 0, expect.any(AbortSignal))
  await wrapper.get('input[placeholder="例如 10.00"]').setValue('1.00')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(createWalletTopUp).toHaveBeenCalledWith(expect.objectContaining({ currency: 'USD', amount_minor: 100 }))
  wrapper.unmount()
})
it('retains an unresolved top-up currency when the platform default changes', async () => {
  vi.mocked(createWalletTopUp).mockRejectedValueOnce(new Error('response lost'))
  const first = await open()
  await first.wrapper.get('input[placeholder="例如 10.00"]').setValue('1.00')
  await first.wrapper.get('form').trigger('submit'); await flushPromises()
  const request = vi.mocked(createWalletTopUp).mock.calls[0]![0]
  first.wrapper.unmount()
  state.publicConfigs = [{ config_key: 'default_currency', value: 'USD' }]
  const second = await open()
  expect(second.wrapper.get('#wallet-currency').text()).toContain('CNY')
  expect(second.wrapper.get('#wallet-currency').attributes('disabled')).toBeDefined()
  await second.wrapper.get('form').trigger('submit'); await flushPromises()
  expect(vi.mocked(createWalletTopUp).mock.calls[1]![0]).toEqual(request)
  second.wrapper.unmount()
})


it('fills a shortcut amount without creating an order until the user submits', async () => {
  const { wrapper } = await open()
  await wrapper.get('[aria-label="快捷充值金额"] button:nth-child(2)').trigger('click')
  expect((wrapper.get('input[placeholder="例如 10.00"]').element as HTMLInputElement).value).toBe('30')
  expect(createWalletTopUp).not.toHaveBeenCalled()
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(createWalletTopUp).toHaveBeenCalledWith(expect.objectContaining({ amount_minor: 3000, currency: 'CNY' }))
  wrapper.unmount()
})
