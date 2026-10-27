import { defineComponent, reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import NodeObservationQueue from './NodeObservationQueue.vue'
const mocks = vi.hoisted(() => ({ read: vi.fn(), retry: vi.fn(), app: null as any }))
vi.mock('../api/nodeObservationIntents', () => ({ fetchObservationIntents: mocks.read, retryObservationIntent: mocks.retry }))
vi.mock('../stores/app', () => ({ useAppStore: () => mocks.app }))
beforeEach(() => { vi.resetAllMocks(); mocks.app = reactive({ user: { id: 1, isAdmin: true } }) })
const drawer = defineComponent({ props: ['open'], template: '<aside v-if="open"><slot /></aside>' })
const select = defineComponent({ props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :value="option.value">{{ option.label }}</option></select>' })
describe('retired observation material', () => {
  it.each(['retention_expired', 'capacity_exceeded', 'source_size_limit', '__proto__'])('shows %s as a coverage gap and never offers retry or zero-load status', async reason => {
    mocks.read.mockResolvedValueOnce({ status: 'held', has_more: false, items: [] }).mockResolvedValueOnce({ status: 'retired', has_more: false, items: [{ id: 'a'.repeat(64), node_id: '9', status: 'retired', reason: 'unknown_source', asset_relation: 'unknown', attempts: '1', retained_bytes: '61440', material_bytes: '3000000', part_count: '49', material_retired_at: '2026-10-27T00:00:00Z', material_cleanup_state: 'deleting', material_gap_reason: reason, coverage_gap: true, replay_unavailable: true, material_verified: false, enqueued_at: '2026-10-01T00:00:00Z', available_at: '2026-10-01T00:00:00Z', disposition_updated_at: '2026-10-02T00:00:00Z' }] })
    const wrapper = mount(NodeObservationQueue, { props: { open: true }, global: { stubs: { DetailDrawer: drawer, UiSelect: select } } })
    await flushPromises(); await wrapper.get('select').setValue('retired'); await flushPromises()
    expect(mocks.read).toHaveBeenLastCalledWith('retired', undefined)
    expect(wrapper.text()).toContain('观察覆盖有缺口')
    expect(wrapper.text()).toContain('不能据此判断当前负载为零')
    expect(wrapper.text()).toContain('登记保留 61440 字节')
    expect(wrapper.text()).toContain('可选原材料已退出重放窗口')
    expect(wrapper.findAll('button').some(button => button.text() === '核对重试')).toBe(false)
    expect(mocks.retry).not.toHaveBeenCalled()
    if (reason === '__proto__') expect(wrapper.text()).toContain('来源材料不可用，需核对')
    wrapper.unmount()
  })
})
