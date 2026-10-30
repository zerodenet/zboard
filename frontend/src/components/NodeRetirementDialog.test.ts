import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import NodeRetirementDialog from './NodeRetirementDialog.vue'
const api = vi.hoisted(() => ({ read: vi.fn(), start: vi.fn() }))
vi.mock('../api/nodeRetirement', () => ({ fetchNodeRetirement: api.read, startNodeRetirement: api.start }))
const value = (state: string, mode = 'delete') => ({ node_id: 7, node_name: 'test-node', mode, state, attempt: 1, run_id: 'run-7', steps: [{ id: 1, kind: 'dns', label: '删除 DNS：node.example.test', state: state === 'failed' ? 'failed' : state === 'succeeded' ? 'succeeded' : 'pending' }] })
function open() { return mount(NodeRetirementDialog, { props: { open: true, nodeId: 7, nodeName: 'test-node' }, global: { stubs: { ModalDialog: { props: ['open', 'title'], template: '<section v-if="open"><h2>{{ title }}</h2><slot/><slot name="footer"/></section>' } } } }) }
describe('node deletion progress', () => {
  beforeEach(() => { vi.clearAllMocks(); api.read.mockResolvedValue(null); api.start.mockResolvedValue(value('queued')) })
  afterEach(() => vi.useRealTimers())
  it('has one delete action covering provider DNS and reports unavailable SSH separately', async () => {
    vi.useFakeTimers()
    const w = open(); await flushPromises()
    expect(w.text()).toContain('删除面板管理')
    expect(w.text()).not.toMatch(/退役|移除面板管理|归档/)
    expect(w.findAll('input[type="radio"]')).toHaveLength(0)
    await w.findAll('button').find(b => b.text() === '确认删除节点')!.trigger('click'); await flushPromises()
    expect(api.start).toHaveBeenCalledWith(7, 'delete', expect.any(AbortSignal))
    api.read.mockResolvedValue({ ...value('succeeded'), steps: [...value('succeeded').steps, { id: 7, kind: 'zero', label: '清理远端 Zero', state: 'unconfirmed', message: 'SSH 不可用，远端程序和文件未能清理。' }] })
    await vi.advanceTimersByTimeAsync(2000); await flushPromises()
    expect(w.emitted('completed')).toHaveLength(1)
    expect(w.text()).toContain('节点已删除')
    expect(w.text()).toContain('远端清理未确认')
    expect(w.text()).toContain('SSH 不可用')
    w.unmount()
  })
  it('retries failed provider deletion without offering to retain its DNS', async () => {
    api.read.mockResolvedValue({ ...value('failed'), last_error: '供应商权限不足，DNS 删除未完成。' })
    const w = open(); await flushPromises()
    expect(w.text()).toContain('DNS 删除未完成')
    await w.findAll('button').find(b => b.text() === '重试未完成项目')!.trigger('click'); await flushPromises()
    expect(api.start).toHaveBeenCalledWith(7, 'delete', expect.any(AbortSignal))
    w.unmount()
  })
  it('does not report a historical detach as completion of provider deletion', async () => {
    api.read.mockResolvedValue(value('succeeded', 'detach'))
    const w = open(); await flushPromises()
    expect(w.emitted('completed')).toBeUndefined()
    expect(w.text()).toContain('继续清理供应商 DNS')
    await w.findAll('button').find(b => b.text() === '确认删除节点')!.trigger('click'); await flushPromises()
    expect(api.start).toHaveBeenCalledWith(7, 'delete', expect.any(AbortSignal))
    w.unmount()
  })
  it('prevents duplicate deletion while work is active', async () => {
    api.read.mockResolvedValue(value('running'))
    const w = open(); await flushPromises()
    const submit = w.findAll('button').find(b => b.text() === '确认删除节点')!
    expect(submit.attributes('disabled')).toBeDefined()
    await submit.trigger('click'); expect(api.start).not.toHaveBeenCalled()
    w.unmount()
  })
  it('discards a late response after closing without canceling submitted deletion', async () => {
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
