import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { updateSubscriptionQuota } from '../api/client'
import SubscriptionQuotaEditor from './SubscriptionQuotaEditor.vue'
import ByteSizeInput from './ByteSizeInput.vue'
import UiSelect from './UiSelect.vue'

vi.mock('../api/client', () => ({ updateSubscriptionQuota: vi.fn() }))
const sub = { id: 31, flow_total: 100, flow_used: 100, reset_quota_bytes: 100 } as any
const field = defineComponent({ template: '<label><slot :control-attrs="{}" /></label>' })
function render() { return mount(SubscriptionQuotaEditor, { props: { subscription: sub }, global: { stubs: { FormField: field } } }) }
beforeEach(() => vi.resetAllMocks())
it('sends only explicitly edited values without page snapshots', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const wrapper = render()
  wrapper.findAllComponents(ByteSizeInput)[0].vm.$emit('update:modelValue', 200)
  wrapper.findAllComponents(ByteSizeInput)[1].vm.$emit('update:modelValue', 0)
  await wrapper.get('textarea').setValue('客服修正用量')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(31, { flow_total: 200, flow_used: 0, reason: '客服修正用量', idempotency_key: expect.any(String) })
  expect(wrapper.emitted('saved')).toHaveLength(1)
  wrapper.unmount()
})
it('retries an uncertain network result with the same idempotency key', async () => {
  vi.mocked(updateSubscriptionQuota).mockRejectedValue(new Error('network'))
  const wrapper = render(); wrapper.findAllComponents(ByteSizeInput)[0].vm.$emit('update:modelValue', 200)
  await wrapper.get('textarea').setValue('重试调整操作')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(vi.mocked(updateSubscriptionQuota).mock.calls[1][1].idempotency_key).toBe(vi.mocked(updateSubscriptionQuota).mock.calls[0][1].idempotency_key)
  wrapper.unmount()
})
it('preserves live usage when only the quota is edited', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const wrapper = render()
  wrapper.findAllComponents(ByteSizeInput)[0].vm.$emit('update:modelValue', 200)
  await wrapper.get('textarea').setValue('增加当前配额')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  const payload = vi.mocked(updateSubscriptionQuota).mock.calls[0][1]
  expect(payload).toMatchObject({ flow_total: 200 })
  expect(payload).not.toHaveProperty('flow_used')
  expect(payload).not.toHaveProperty('reset_quota_bytes')
  wrapper.unmount()
})
it('does not save a reason without any parameter changes', async () => {
  const wrapper = render(); await wrapper.get('textarea').setValue('没有修改参数')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('changes only the future reset policy without overwriting quota or live usage', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const wrapper = render()
  wrapper.getComponent(UiSelect).vm.$emit('update:modelValue', 2)
  await wrapper.get('textarea').setValue('修正历史重置配置')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(31, { reset_policy: 2, reason: '修正历史重置配置', idempotency_key: expect.any(String) })
  expect(wrapper.text()).toContain('当前用量不清零')
  wrapper.unmount()
})
it('can explicitly repair a missing date while keeping the same periodic policy', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const wrapper = mount(SubscriptionQuotaEditor, { props: { subscription: { ...sub, reset_policy: 2, next_reset_at: null } }, global: { stubs: { FormField: field } } })
  const recalculate = wrapper.findAll('button').find(button => button.text() === '重新计算')!
  await recalculate.trigger('click')
  await wrapper.get('textarea').setValue('补全重置日期')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(31, { reset_policy: 2, reason: '补全重置日期', idempotency_key: expect.any(String) })
  wrapper.unmount()
})
const scheduled = () => ({ ...sub, status: 'active', start_at: '2026-01-31T09:30:00Z', end_at: '2031-01-31T09:30:49.123Z', reset_policy: 2, next_reset_at: '2030-05-01T09:30:23.456Z' })
it('sends only edited expiry with the original date baseline', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const original = scheduled(), wrapper = mount(SubscriptionQuotaEditor, { props: { subscription: original } })
  await wrapper.get('#subscription-end-at').setValue('2031-02-10T10:15')
  await wrapper.get('textarea').setValue('客服延长有效期')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(31, { end_at: '2031-02-10T10:15:00.000Z', expected_end_at: original.end_at, reason: '客服延长有效期', idempotency_key: expect.any(String) })
  wrapper.unmount()
})
it('sets the exact next reset date without changing quota or usage', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const original = scheduled(), wrapper = mount(SubscriptionQuotaEditor, { props: { subscription: original, mode: 'reset' } })
  expect(wrapper.find('#subscription-end-at').exists()).toBe(false)
  await wrapper.get('#subscription-next-reset-at').setValue('2030-05-03T12:45')
  await wrapper.get('textarea').setValue('修正下次重置日期')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(31, { next_reset_at: '2030-05-03T12:45:00.000Z', expected_next_reset_at: original.next_reset_at, reason: '修正下次重置日期', idempotency_key: expect.any(String) })
  wrapper.unmount()
})
it('does not truncate unchanged date seconds when only quota changes', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const wrapper = mount(SubscriptionQuotaEditor, { props: { subscription: scheduled() } })
  wrapper.findAllComponents(ByteSizeInput)[0].vm.$emit('update:modelValue', 300)
  await wrapper.get('textarea').setValue('仅增加当前配额')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(31, { flow_total: 300, reason: '仅增加当前配额', idempotency_key: expect.any(String) })
  wrapper.unmount()
})
it('validates cleared expiry and a reset after the edited term', async () => {
  const wrapper = mount(SubscriptionQuotaEditor, { props: { subscription: scheduled() } })
  await wrapper.get('#subscription-end-at').setValue('')
  await wrapper.get('textarea').setValue('填写日期测试')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).not.toHaveBeenCalled()
  expect(wrapper.text()).toContain('有效日期')
  await wrapper.get('#subscription-end-at').setValue('2030-05-02T10:00')
  await wrapper.get('#subscription-next-reset-at').setValue('2030-05-03T12:45')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).not.toHaveBeenCalled()
  expect(wrapper.text()).toContain('不晚于到期时间')
  wrapper.unmount()
})
