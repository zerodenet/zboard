import PrimeVue from 'primevue/config'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAccountSubscriptionsPage } from '../../api/client'
import TablePager from '../../components/TablePager.vue'
import AccountSubscriptionFilter from './AccountSubscriptionFilter.vue'

vi.mock('../../api/client', () => ({ fetchAccountSubscriptionsPage: vi.fn() }))
function page(id: number, total = 150) { return { items: [{ id, plan_name: `Historical ${id}`, sku_name: 'Expired plan' }], total } as any }
function deferred() {
  let resolve!: (value: any) => void, reject!: (error: Error) => void
  const promise = new Promise<any>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
describe('account subscription filter bounded reads', () => {
  let wrapper: VueWrapper | undefined
  beforeEach(() => { vi.resetAllMocks(); vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page(150)) })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })
  async function create(value = '') {
    wrapper = mount(AccountSubscriptionFilter, { props: { modelValue: value,
      'onUpdate:modelValue': value => { void wrapper!.setProps({ modelValue: value }) },
    }, global: { plugins: [PrimeVue], stubs: { teleport: true } } })
    await flushPromises()
  }
  async function open() { await wrapper!.get('.workbench-filter-chip-trigger').trigger('click'); await flushPromises() }
  it('resolves a deep link outside the first hundred without eagerly listing options', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page(1))
    await create('1')
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledOnce()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith({ subscriptionId: 1, limit: 1 }, expect.anything())
    expect(wrapper!.text()).toContain('Historical 1')
    await open()
    expect(fetchAccountSubscriptionsPage).toHaveBeenLastCalledWith({ q: '', offset: 0, limit: 25 }, expect.anything())
  })
  it('paginates historical options and chooses an item outside the preview', async () => {
    await create()
    expect(fetchAccountSubscriptionsPage).not.toHaveBeenCalled()
    await open()
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page(25))
    wrapper!.getComponent(TablePager).vm.$emit('change', { offset: 125, limit: 25 })
    await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenLastCalledWith({ q: '', offset: 125, limit: 25 }, expect.anything())
    await wrapper!.findAll('[role="option"]').find(option => option.text().includes('Historical 25'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.emitted('update:modelValue')?.at(-1)).toEqual(['25'])
    expect(wrapper!.text()).toContain('Historical 25')
    expect(wrapper!.emitted('apply')).toHaveLength(1)
  })
  it('searches on the server from page zero and rejects a superseded options response', async () => {
    const late = deferred()
    vi.mocked(fetchAccountSubscriptionsPage).mockReturnValueOnce(late.promise).mockResolvedValueOnce(page(1, 1))
    await create(); await open()
    await wrapper!.get('input[aria-label="搜索订阅"]').setValue('Historical 1')
    await wrapper!.get('input[aria-label="搜索订阅"]').trigger('keyup.enter')
    await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenLastCalledWith({ q: 'Historical 1', offset: 0, limit: 25 }, expect.anything())
    late.resolve(page(150))
    await flushPromises()
    expect(wrapper!.text()).toContain('Historical 1')
    expect(wrapper!.text()).not.toContain('Historical 150')
    expect(wrapper!.getComponent(TablePager).props('total')).toBe(1)
    expect(vi.mocked(fetchAccountSubscriptionsPage).mock.calls[0]![1]?.signal?.aborted).toBe(true)
  })
  it('distinguishes missing selection and option read failure from an empty list', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValueOnce({ items: [], total: 0 } as any)
      .mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(page(10))
    await create('999')
    expect(wrapper!.text()).toContain(`#${999}（不存在或不可访问）`)
    await open()
    expect(wrapper!.text()).toContain('订阅选项加载失败')
    expect(wrapper!.text()).not.toContain('没有匹配的订阅')
    await wrapper!.findAll('button').find(button => button.text().includes('重试订阅选项'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('Historical 10')
    expect(wrapper!.text()).toContain(`#${999}（不存在或不可访问）`)
    expect(wrapper!.emitted('update:modelValue')).toBeUndefined()
  })
  it('isolates deep-link races and aborts both pending readers on unmount', async () => {
    const old = deferred(), current = deferred(), options = deferred()
    vi.mocked(fetchAccountSubscriptionsPage).mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise).mockReturnValueOnce(options.promise)
    await create('1')
    await wrapper!.setProps({ modelValue: '2' }); await flushPromises()
    old.resolve(page(1)); await flushPromises()
    expect(wrapper!.text()).not.toContain('Historical 1')
    expect(wrapper!.text()).toContain('#2（加载中）')
    await open()
    wrapper!.unmount(); wrapper = undefined
    expect(vi.mocked(fetchAccountSubscriptionsPage).mock.calls.every(call => call[1]?.signal?.aborted)).toBe(true)
    current.resolve(page(2)); options.resolve(page(150)); await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledTimes(3)
  })
  it('never turns a malformed deep-link ID into a lookup for another subscription', async () => {
    await create('0x10')
    expect(fetchAccountSubscriptionsPage).not.toHaveBeenCalled()
    expect(wrapper!.text()).toContain('读取失败')
    await wrapper!.setProps({ modelValue: '150' }); await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith({ subscriptionId: 150, limit: 1 }, expect.anything())
    expect(wrapper!.text()).toContain('Historical 150')
  })
})
