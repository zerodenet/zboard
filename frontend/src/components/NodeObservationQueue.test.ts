import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import NodeObservationQueue from './NodeObservationQueue.vue'
import type { ObservationIntentPage, ObservationIntentStatus } from '../api/nodeObservationIntents'
const mocks = vi.hoisted(() => ({ read: vi.fn() }))
vi.mock('../api/nodeObservationIntents', () => ({ fetchObservationIntents: mocks.read }))
const drawer = defineComponent({ props: ['open'], template: '<aside v-if="open"><slot /></aside>' })
const select = defineComponent({ props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :value="option.value">{{ option.label }}</option></select>' })
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail }); return { promise, resolve, reject } }
function page(status: ObservationIntentStatus = 'held', id = 'a'.repeat(64), more = true): ObservationIntentPage {
  return { status, has_more: more, ...(more ? { next_cursor: `cursor-${id}` } : {}), items: [{
    id, node_id: '18446744073709551615', status, reason: 'unknown_source', asset_relation: 'node_missing', attempts: '4', material_bytes: '1048576', part_count: '18', material_verified: false,
    enqueued_at: '2026-10-20T01:00:00Z', available_at: '2026-10-21T02:00:00Z', disposition_updated_at: '2026-10-21T03:00:00Z',
  }] }
}
function render(open = true) { return mount(NodeObservationQueue, { props: { open }, global: { stubs: { DetailDrawer: drawer, UiSelect: select } } }) }
afterEach(() => { vi.resetAllMocks() })
describe('global observation queue', () => {
  it('loads only on open and explicit actions; follows the same-status cursor', async () => {
    mocks.read.mockResolvedValueOnce(page()).mockResolvedValueOnce(page('held', 'b'.repeat(64), false))
    const wrapper = render(false)
    expect(mocks.read).not.toHaveBeenCalled()
    await wrapper.setProps({ open: true }); await flushPromises()
    expect(mocks.read).toHaveBeenNthCalledWith(1, 'held', undefined)
    expect(wrapper.text()).toContain('18446744073709551615')
    expect(wrapper.text()).toContain('当前节点已不存在')
    const next = wrapper.findAll('button').find(button => button.text() === '下一页')!
    await next.trigger('click'); await flushPromises()
    expect(mocks.read).toHaveBeenNthCalledWith(2, 'held', `cursor-${'a'.repeat(64)}`)
    expect(wrapper.text()).toContain('b'.repeat(64))
    expect(wrapper.findAll('button').some(button => button.text() === '下一页')).toBe(false)
    await flushPromises(); expect(mocks.read).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
  it.each(['9007199254740993', '18446744073709551615'])('displays diagnostic decimal %s without Number coercion', async value => {
    const result = page('held', 'd'.repeat(64), false)
    result.items[0].attempts = value
    result.items[0].material_bytes = value
    result.items[0].part_count = value
    mocks.read.mockResolvedValueOnce(result)
    const wrapper = render(); await flushPromises()
    const diagnostics = wrapper.get('tbody td:nth-child(4)').text()
    expect(diagnostics).toContain(`${value} 字节 / ${value} 段`)
    expect(diagnostics).toContain(`领取尝试 ${value}`)
    expect(wrapper.text()).toContain('未重新验证保留材料')
    wrapper.unmount()
  })
  it('uses a fixed fallback for an unfamiliar asset relation', async () => {
    const result = page()
    result.items[0].asset_relation = 'toString' as ObservationIntentPage['items'][number]['asset_relation']
    mocks.read.mockResolvedValueOnce(result)
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('tbody td:nth-child(3)').text()).toBe('资产关系未知')
    wrapper.unmount()
  })
  it('does not let late requests overwrite a different selected status', async () => {
    const old = deferred<ObservationIntentPage>(), current = deferred<ObservationIntentPage>()
    mocks.read.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = render()
    await wrapper.find('select').setValue('pending')
    expect(mocks.read).toHaveBeenLastCalledWith('pending', undefined)
    current.resolve(page('pending', 'p'.repeat(64), false)); await flushPromises()
    old.resolve(page('held', 'h'.repeat(64))); await flushPromises()
    expect(wrapper.text()).toContain('p'.repeat(64)); expect(wrapper.text()).not.toContain('h'.repeat(64))
    expect(wrapper.text()).toContain('已加载状态：待处理 / 自动重试')
    wrapper.unmount()
  })
  it('retains the known page and its status on filter and pagination errors', async () => {
    mocks.read.mockResolvedValueOnce(page()).mockRejectedValueOnce(new Error('private SQL detail')).mockRejectedValueOnce(new Error('offline'))
    const wrapper = render(); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '下一页')!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('a'.repeat(64)); expect(wrapper.get('[role="alert"]').text()).toContain('读取失败')
    await wrapper.find('select').setValue('pending'); await flushPromises()
    expect(wrapper.text()).toContain('已加载状态：已保留待核对')
    expect(wrapper.text()).toContain('当前显示上一次成功加载的记录')
    expect(wrapper.findAll('button').find(button => button.text() === '下一页')!.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('private SQL detail')
    wrapper.unmount()
  })
  it('ignores an earlier open request after closing and reopening', async () => {
    const old = deferred<ObservationIntentPage>()
    mocks.read.mockReturnValueOnce(old.promise).mockResolvedValueOnce(page('held', 'n'.repeat(64), false))
    const wrapper = render(); await wrapper.setProps({ open: false }); await wrapper.setProps({ open: true }); await flushPromises()
    old.resolve(page('held', 'o'.repeat(64))); await flushPromises()
    expect(wrapper.text()).toContain('n'.repeat(64)); expect(wrapper.text()).not.toContain('o'.repeat(64))
    wrapper.unmount()
  })
  it.each(['<script>private-payload</script>', 'toString', '__proto__'])('names queue times accurately and uses fixed unknown-reason text for %s', async reason => {
    const result = page(); result.items[0].reason = reason
    mocks.read.mockResolvedValue(result)
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('未重新验证保留材料')
    expect(wrapper.text()).toContain('未识别的处置原因')
    expect(wrapper.text()).not.toContain('private-payload')
    expect(wrapper.find('script').exists()).toBe(false)
    for (const label of ['入队', '调度可用', '处置更新']) expect(wrapper.text()).toContain(label)
    expect(wrapper.findAll('time').map(time => time.attributes('datetime'))).toEqual(['2026-10-20T01:00:00.000Z', '2026-10-21T02:00:00.000Z', '2026-10-21T03:00:00.000Z'])
    expect(wrapper.findAll('button').some(button => /恢复|重排|删除|清租约/.test(button.text()))).toBe(false)
    wrapper.unmount()
  })
})
