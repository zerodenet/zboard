import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PaymentReceipts from './PaymentReceipts.vue'
import { fetchPaymentReceipts, resolvePaymentReceipt } from '../api/paymentReceipts'
import UiSelect from './UiSelect.vue'

const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/paymentReceipts', () => ({ fetchPaymentReceipts: vi.fn(), resolvePaymentReceipt: vi.fn() }))
const row = { id: 4, amount_minor: '1000', currency: 'CNY', disposition: 'review_required', created_at: '2026-10-19T00:00:00Z' }
function open(admin = false) { return mount(PaymentReceipts, { props: { orderId: 27, admin }, global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } }) }
beforeEach(() => {
  vi.clearAllMocks(); state.app = reactive({ user: { id: 1 } })
  vi.mocked(fetchPaymentReceipts).mockResolvedValue({ items: [row], has_more: false })
})
describe('cash receipt ownership', () => {
  it('shows an owner the separate cash disposition and never offers manual financial authority', async () => {
    const wrapper = open(); await flushPromises()
    expect(wrapper.text()).toContain('CNY 10.00')
    expect(wrapper.text()).toContain('待人工核对')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(fetchPaymentReceipts).toHaveBeenCalledWith(27, false, 0, expect.any(AbortSignal))
    expect(resolvePaymentReceipt).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('requires a ticket and explicit decision, and does not automatically replay an uncertain financial write', async () => {
    const wrapper = open(true); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '核对这笔收据')!.trigger('click')
    expect(wrapper.get('form button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.findAll('input')[0]!.setValue('12')
    wrapper.getComponent(UiSelect).vm.$emit('update:modelValue', 'credit_wallet')
    await wrapper.findAll('input')[1]!.setValue('渠道及原人工记录已核对，该笔未入账。')
    vi.mocked(resolvePaymentReceipt).mockRejectedValueOnce(new Error('connection lost'))
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(resolvePaymentReceipt).toHaveBeenCalledTimes(1)
    expect(resolvePaymentReceipt).toHaveBeenCalledWith(27, { receipt_id: 4, ticket_id: 12, decision: 'credit_wallet', reason: '渠道及原人工记录已核对，该笔未入账。' })
    await wrapper.findAll('button').find(button => button.text() === '刷新收据')!.trigger('click'); await flushPromises()
    expect(resolvePaymentReceipt).toHaveBeenCalledTimes(1)
    state.app.user.id = 2; await flushPromises()
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('渠道及原人工记录已核对，该笔未入账。')
    wrapper.unmount()
  })
  it('discards an old account response and clears financial drafts', async () => {
    let finish!: (data: any) => void
    vi.mocked(fetchPaymentReceipts).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = open(true); await flushPromises()
    vi.mocked(fetchPaymentReceipts).mockResolvedValue({ items: [], has_more: false })
    state.app.user.id = 2; await flushPromises()
    finish({ items: [row], has_more: false }); await flushPromises()
    expect(wrapper.text()).not.toContain('收据 #4')
    expect(resolvePaymentReceipt).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
