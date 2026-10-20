import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, expect, it, vi } from 'vitest'
import SubscriptionAutomaticRenewal from './SubscriptionAutomaticRenewal.vue'
import { fetchAutomaticRenewal, setAutomaticRenewal } from '../api/automaticRenewal'
import { fetchPlanCatalogSKUs } from '../api/client'

const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/automaticRenewal', () => ({ fetchAutomaticRenewal: vi.fn(), setAutomaticRenewal: vi.fn() }))
vi.mock('../api/client', () => ({ fetchPlanCatalogSKUs: vi.fn() }))
const offer = { plan_sku_id: 3, currency: 'CNY', base_price_minor: '12000', billing_unit: 'year', billing_value: 1, quote_fingerprint: 'a'.repeat(64) }
const initial = { subscription_id: 7, enabled: false, plan_sku_id: 0, revision: 0, state: 'disabled', message: '默认关闭', offer }
const enabled = { ...initial, enabled: true, plan_sku_id: 3, revision: 1, state: 'waiting', message: '已保存，仅使用站内余额', offer: undefined }
function open() { return mount(SubscriptionAutomaticRenewal, { props: { subscriptionId: 7, planId: 2 } }) }
async function click(wrapper: ReturnType<typeof open>, label: string) {
  const button = wrapper.findAll('button').find(button => button.text() === label)
  expect(button, label).toBeDefined()
  await button!.trigger('click'); await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks(); state.app = reactive({ user: { id: 1 } })
  vi.mocked(fetchAutomaticRenewal).mockResolvedValue(initial)
  vi.mocked(setAutomaticRenewal).mockResolvedValue(enabled)
  vi.mocked(fetchPlanCatalogSKUs).mockResolvedValue({ items: [{ id: 3, name: '年付', billing_value: 1, billing_unit: 'year' }], total: 1, offset: 0, limit: 25, page: { total: 1, offset: 0, limit: 25 } } as any)
})
it('reads a quote and requires explicit confirmation before enabling; sends no client amount', async () => {
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).toContain('CNY 120.00')
  expect(setAutomaticRenewal).not.toHaveBeenCalled()
  expect(fetchPlanCatalogSKUs).toHaveBeenCalledWith(2, expect.objectContaining({ operation: 'renew', limit: 25, anchorId: 3 }))
  await click(wrapper, '确认此规格并开启余额自动续费')
  expect(setAutomaticRenewal).toHaveBeenCalledTimes(1)
  expect(setAutomaticRenewal).toHaveBeenLastCalledWith(7, { enabled: true, plan_sku_id: 3, quote_fingerprint: offer.quote_fingerprint, expected_revision: 0 })
  expect(wrapper.text()).toContain('已保存，仅使用站内余额')
  wrapper.unmount()
})
it('displays exact large minor-unit strings and keeps insufficient funds visible', async () => {
  vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...enabled, state: 'insufficient_balance', message: '可用站内余额不足，尚未扣款', offer: { ...offer, base_price_minor: '9007199254740993' } })
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).toContain('CNY 90071992547409.93')
  expect(wrapper.text()).toContain('尚未扣款')
  expect(setAutomaticRenewal).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('can disable existing consent when no safe new offer exists', async () => {
  vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...enabled, state: 'agreement_unavailable', message: '优惠约定待核对' })
  vi.mocked(setAutomaticRenewal).mockResolvedValue({ ...enabled, enabled: false, revision: 2, state: 'disabled', message: '已关闭' })
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).not.toContain('确认此规格并开启')
  await click(wrapper, '关闭余额自动续费')
  expect(setAutomaticRenewal).toHaveBeenCalledTimes(1)
  expect(setAutomaticRenewal).toHaveBeenLastCalledWith(7, { enabled: false, plan_sku_id: 3, quote_fingerprint: '', expected_revision: 1 })
  wrapper.unmount()
})
it('does not replay a lost write after remount or status refresh', async () => {
  let wrapper = open(); await flushPromises()
  vi.mocked(setAutomaticRenewal).mockRejectedValueOnce(new Error('lost response'))
  await click(wrapper, '确认此规格并开启余额自动续费')
  expect(wrapper.text()).toContain('不会自动重放')
  expect(wrapper.text()).not.toContain('确认此规格并开启')
  wrapper.unmount(); vi.mocked(fetchAutomaticRenewal).mockResolvedValue(enabled)
  wrapper = open(); await flushPromises(); await click(wrapper, '核对当前状态')
  expect(setAutomaticRenewal).toHaveBeenCalledTimes(1)
  wrapper.unmount()
})
it('isolates accounts and discards an old account response', async () => {
  let finish!: (value: any) => void
  vi.mocked(fetchAutomaticRenewal).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
  const wrapper = open()
  vi.mocked(fetchAutomaticRenewal).mockRejectedValueOnce(new Error('not owned'))
  state.app.user.id = 2; await flushPromises(); finish(initial); await flushPromises()
  expect(wrapper.text()).not.toContain('CNY 120.00')
  expect(setAutomaticRenewal).not.toHaveBeenCalled()
  wrapper.unmount()
})
