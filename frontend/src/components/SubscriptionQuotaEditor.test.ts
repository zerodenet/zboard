import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { updateSubscriptionQuota } from '../api/client'
import SubscriptionQuotaEditor from './SubscriptionQuotaEditor.vue'
import ByteSizeInput from './ByteSizeInput.vue'

vi.mock('../api/client', () => ({ updateSubscriptionQuota: vi.fn() }))
const sub = { id: 31, flow_total: 100, flow_used: 100, reset_quota_bytes: 100 } as any
const field = defineComponent({ template: '<label><slot :control-attrs="{}" /></label>' })
function render() { return mount(SubscriptionQuotaEditor, { props: { subscription: sub }, global: { stubs: { FormField: field } } }) }
beforeEach(() => vi.resetAllMocks())
it('sends current-cycle bytes and reset quota with the original values', async () => {
  vi.mocked(updateSubscriptionQuota).mockResolvedValue()
  const wrapper = render()
  wrapper.findAllComponents(ByteSizeInput)[0].vm.$emit('update:modelValue', 200)
  wrapper.findAllComponents(ByteSizeInput)[1].vm.$emit('update:modelValue', 0)
  await wrapper.get('textarea').setValue('客服修正用量')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(31, { flow_total: 200, flow_used: 0, reset_quota_bytes: 100, expected_flow_total: 100, expected_flow_used: 100, expected_reset_quota_bytes: 100, reason: '客服修正用量', idempotency_key: expect.any(String) })
  expect(wrapper.emitted('saved')).toHaveLength(1)
  wrapper.unmount()
})
it('retries an uncertain network result with the same idempotency key', async () => {
  vi.mocked(updateSubscriptionQuota).mockRejectedValue(new Error('network'))
  const wrapper = render(); await wrapper.get('textarea').setValue('重试调整操作')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(vi.mocked(updateSubscriptionQuota).mock.calls[1][1].idempotency_key).toBe(vi.mocked(updateSubscriptionQuota).mock.calls[0][1].idempotency_key)
  wrapper.unmount()
})
it('requires a refresh after usage changes instead of overwriting it', async () => {
  vi.mocked(updateSubscriptionQuota).mockRejectedValue({ response: { status: 409, data: { message: '流量已变化' } } })
  const wrapper = render(); await wrapper.get('textarea').setValue('调整当前流量')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.text()).toContain('流量已变化')
  expect(wrapper.find('button[type="submit"]').attributes('disabled')).toBeDefined()
  const refresh = wrapper.findAll('button').find(button => button.text() === '刷新详情')!
  await refresh.trigger('click'); expect(wrapper.emitted('refresh')).toHaveLength(1)
  wrapper.unmount()
})
