import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, expect, it, vi } from 'vitest'
import SubscriptionAutomaticRenewal from './SubscriptionAutomaticRenewal.vue'
import { fetchAutomaticRenewal, setAutomaticRenewal } from '../api/automaticRenewal'
import { fetchPlanCatalogSKUs } from '../api/client'
import type { AutomaticRenewalOffer } from '../api/automaticRenewal'

const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/automaticRenewal', () => ({ fetchAutomaticRenewal: vi.fn(), setAutomaticRenewal: vi.fn() }))
vi.mock('../api/client', () => ({ fetchPlanCatalogSKUs: vi.fn() }))
const offer = { plan_sku_id: 3, currency: 'CNY', base_price_minor: '12000', billing_unit: 'year', billing_value: 1, quote_fingerprint: 'a'.repeat(64) }
const initial = { subscription_id: 7, enabled: false, plan_sku_id: 0, revision: 0, state: 'disabled', message: '默认关闭', offer }
const enabled = { ...initial, enabled: true, plan_sku_id: 3, revision: 1, state: 'waiting', message: '已保存，仅使用站内余额', offer: undefined }
function open() { return mount(SubscriptionAutomaticRenewal, { props: { subscriptionId: 7, planId: 2 }, global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } }) }
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

it('keeps a paused historical item readable and revocable without an unsafe quote', async () => {
 vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...enabled, state: 'history_review_required', message: '历史超过安全核对上限，本次未扣款' })
 vi.mocked(setAutomaticRenewal).mockResolvedValue({ ...enabled, enabled: false, revision: 2, state: 'disabled', message: '已关闭' })
 const wrapper = open(); await flushPromises(); expect(wrapper.text()).toContain('本项自动扣款已暂停'); expect(wrapper.text()).toContain('本次未扣款')
 await click(wrapper, '关闭余额自动续费'); expect(setAutomaticRenewal).toHaveBeenCalledWith(7, expect.objectContaining({ enabled: false, expected_revision: 1 })); wrapper.unmount()
})

function recurringOffer(): AutomaticRenewalOffer {
  return { ...offer, agreement_version: 2, discount_amount_minor: '2000', payable_amount_minor: '10000',
    discount: { provider: { plugin_id: 'native.quote', signing_key_fingerprint: 'private-signature', package_digest: 'private-package', generation: 1, config_revision: 1 }, offer: { status: 'available', currency: 'CNY', discount_minor: 2000, input_fingerprint: 'private-context', expires_at: '2099-01-01T00:00:00Z', message: '原长期优惠', agreement: { id: 'long', version: '1', scope: 'recurring', terms_hash: 'private-terms-hash', terms_json: '{"private":"rules"}' } } } }
}
it('shows recurring base, reduction and actual automatic debit before explicit consent without sending pricing evidence', async () => {
  vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...initial, offer: recurringOffer() })
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).toContain('基础续费价格为 CNY 120.00')
  expect(wrapper.text()).toContain('本次核对优惠 CNY 20.00')
  expect(wrapper.text()).toContain('每次仅从可用站内余额扣除 CNY 100.00')
  expect(wrapper.text()).toContain('不会按基础价扣款')
  expect(wrapper.text()).not.toMatch(/private-|"private"/)
  expect(setAutomaticRenewal).not.toHaveBeenCalled()
  await click(wrapper, '确认此规格并开启余额自动续费')
  expect(setAutomaticRenewal).toHaveBeenCalledTimes(1)
  expect(setAutomaticRenewal).toHaveBeenLastCalledWith(7, { enabled: true, plan_sku_id: 3, quote_fingerprint: offer.quote_fingerprint, expected_revision: 0 })
  wrapper.unmount()
})
it('retains revocation when a malformed recurring quote cannot be approved', async () => {
  const invalid = recurringOffer(); invalid.payable_amount_minor = '12000'
  vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...enabled, offer: invalid, state: 'discount_unavailable', message: '优惠不可用，本次不扣款' })
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).toContain('长期优惠自动续费报价无法确认')
  expect(wrapper.text()).not.toContain('确认此规格并开启')
  await click(wrapper, '关闭余额自动续费')
  expect(setAutomaticRenewal).toHaveBeenCalledTimes(1)
  expect(setAutomaticRenewal).toHaveBeenLastCalledWith(7, { enabled: false, plan_sku_id: 3, quote_fingerprint: '', expected_revision: 1 })
  wrapper.unmount()
})
it('expires a recurring approval without replaying a write or disabling current consent', async () => {
  vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
  const value = recurringOffer(); value.discount!.offer.expires_at = new Date(Date.now() + 1000).toISOString()
  vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...enabled, offer: value })
  const wrapper = open()
  try {
    await flushPromises()
    expect(wrapper.text()).toContain('确认此规格并开启')
    await vi.advanceTimersByTimeAsync(1100); await flushPromises()
    expect(wrapper.text()).toContain('优惠报价已过期')
    expect(wrapper.text()).not.toContain('确认此规格并开启')
    expect(wrapper.text()).toContain('关闭余额自动续费')
    expect(setAutomaticRenewal).not.toHaveBeenCalled()
  } finally { wrapper.unmount(); vi.useRealTimers() }
})


it('shows a queued order as uncharged and refresh only reads its state', async () => {
 vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...enabled, state: 'discount_reserved', message: '原订单优惠已确认，尚未自动扣款。', pending_order_id: 91, offer: undefined })
 const wrapper = open(); await flushPromises()
 expect(wrapper.text()).toContain('待处理续费订单 #91')
 expect(wrapper.get('a[href="/account/orders/91"]').text()).toBe('#91')
 expect(wrapper.text()).toContain('不表示已扣款')
 expect(wrapper.text()).not.toContain('最近成功自动续费订单 #91')
 expect(setAutomaticRenewal).not.toHaveBeenCalled()
 await click(wrapper, '核对当前状态')
 expect(fetchAutomaticRenewal).toHaveBeenCalledTimes(2)
 expect(setAutomaticRenewal).not.toHaveBeenCalled()
 wrapper.unmount()
})
it('keeps disabled pending cleanup distinct from another debit and isolates account changes', async () => {
 vi.mocked(fetchAutomaticRenewal).mockResolvedValue({ ...enabled, enabled: false, state: 'disabled', message: '当前许可已关闭，旧待处理订单不会自动扣款，正在关闭旧预留。', pending_order_id: 92, offer: undefined })
 const wrapper = open(); await flushPromises()
 expect(wrapper.text()).toContain('已关闭')
 expect(wrapper.text()).toContain('不会自动扣款')
 expect(wrapper.text()).toContain('正在关闭旧预留')
 expect(wrapper.text()).toContain('待处理续费订单 #92')
 expect(setAutomaticRenewal).not.toHaveBeenCalled()
 vi.mocked(fetchAutomaticRenewal).mockRejectedValue(new Error('foreign pending order'))
 state.app.user.id = 2; await flushPromises()
 expect(wrapper.text()).not.toContain('待处理续费订单 #92')
 expect(setAutomaticRenewal).not.toHaveBeenCalled()
 wrapper.unmount()
})

it('emits one affected subscription only after a confirmed save, never after read-only refresh or failed save', async () => {
  const wrapper = open(); await flushPromises()
  expect(wrapper.emitted('changed')).toBeUndefined()
  await click(wrapper, '确认此规格并开启余额自动续费')
  expect(wrapper.emitted('changed')).toEqual([[7]])
  await click(wrapper, '核对当前状态')
  expect(wrapper.emitted('changed')).toEqual([[7]])
  vi.mocked(setAutomaticRenewal).mockRejectedValueOnce(new Error('unconfirmed save'))
  await click(wrapper, '确认此规格并开启余额自动续费')
  expect(wrapper.emitted('changed')).toEqual([[7]])
  wrapper.unmount()
})

it('does not emit an old subscription or account save after context changes or unmount', async () => {
  for (const change of ['account', 'subscription', 'unmount'] as const) {
    let resolve!: (value: any) => void
    vi.mocked(fetchAutomaticRenewal).mockResolvedValue(initial)
    vi.mocked(setAutomaticRenewal).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = open(); await flushPromises()
    await click(wrapper, '确认此规格并开启余额自动续费')
    if (change === 'account') { state.app.user.id += 1; await flushPromises() }
    else if (change === 'subscription') await wrapper.setProps({ subscriptionId: 8 })
    else wrapper.unmount()
    resolve(enabled); await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
    if (change !== 'unmount') wrapper.unmount()
  }
})
