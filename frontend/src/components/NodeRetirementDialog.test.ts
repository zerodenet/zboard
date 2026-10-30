import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import NodeRetirementDialog from './NodeRetirementDialog.vue'
const api = vi.hoisted(() => ({ read: vi.fn(), start: vi.fn() }))
vi.mock('../api/nodeRetirement', () => ({ fetchNodeRetirement: api.read, startNodeRetirement: api.start }))
const value = (state: string, mode = 'retire') => ({ node_id: 7, node_name: 'test-node', mode, state, attempt: 1, run_id: 'run-7', steps: [{ id: 1, kind: 'dns', label: 'DNS：node.example.test', state: state === 'failed' ? 'failed' : state === 'succeeded' ? 'succeeded' : 'pending' }] })
function open() { return mount(NodeRetirementDialog, { props: { open: true, nodeId: 7, nodeName: 'test-node' }, global: { stubs: { ModalDialog: { props: ['open'], template: '<section v-if="open"><slot/><slot name="footer"/></section>' } } } }) }
describe('node retirement progress', () => {
  beforeEach(() => { vi.clearAllMocks(); api.read.mockResolvedValue(null); api.start.mockResolvedValue(value('queued')) })
  afterEach(() => vi.useRealTimers())
  it('submits explicit detach and polls until completion without calling remote operations from the browser', async () => {
    vi.useFakeTimers()
    const w = open(); await flushPromises()
    await w.get('input[value="detach"]').setValue()
    api.start.mockResolvedValue(value('queued', 'detach'))
    await w.findAll('button').find(b => b.text() === '确认仅移除面板管理')!.trigger('click'); await flushPromises()
    expect(api.start).toHaveBeenCalledWith(7, 'detach', expect.any(AbortSignal))
    expect(w.text()).toContain('排队中')
    api.read.mockResolvedValue(value('succeeded', 'detach'))
    await vi.advanceTimersByTimeAsync(2000); await flushPromises()
    expect(w.emitted('completed')).toHaveLength(1)
    expect(w.text()).toContain('归档完成')
    w.unmount()
  })
  it('restores a failed task and retries the same node with retained evidence visible', async () => {
    api.read.mockResolvedValue({ ...value('failed'), last_error: 'DNS 未完成；记录已保留。' })
    const w = open(); await flushPromises()
    expect(w.text()).toContain('记录已保留')
    expect(w.text()).toContain('DNS：node.example.test')
    await w.findAll('button').find(b => b.text() === '重试未完成步骤')!.trigger('click'); await flushPromises()
    expect(api.start).toHaveBeenCalledWith(7, 'retire', expect.any(AbortSignal))
    w.unmount()
  })
  it('shows shared resources as retained and prevents duplicate submissions while work is active', async () => {
    api.read.mockResolvedValue({ ...value('running'), steps: [{ id: 1, kind: 'dns', label: 'DNS', state: 'retained', message: '共享资源保留' }] })
    const w = open(); await flushPromises()
    expect(w.text()).toContain('共享资源保留')
    const submit = w.findAll('button').find(b => b.text() === '确认退役并清理')!
    expect(submit.attributes('disabled')).toBeDefined()
    await submit.trigger('click'); expect(api.start).not.toHaveBeenCalled()
    w.unmount()
  })
  it('discards a late response after closing and stops polling without canceling the task', async () => {
    vi.useFakeTimers()
    let resolve!: (value: any) => void
    api.read.mockReturnValue(new Promise(done => { resolve = done }))
    const w = open(); await flushPromises()
    await w.setProps({ open: false })
    resolve(value('succeeded')); await flushPromises()
    await vi.advanceTimersByTimeAsync(5000)
    expect(w.emitted('completed')).toBeUndefined()
    expect(api.read).toHaveBeenCalledOnce()
    w.unmount()
  })
})
