import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import NodeKernelUpload from './NodeKernelUpload.vue'
import UiFileUpload from './UiFileUpload.vue'
const mocks = vi.hoisted(() => ({ upload: vi.fn(), confirm: vi.fn() }))
vi.mock('../api/nodeKernelUpload', () => ({ uploadNodeKernel: mocks.upload }))
vi.mock('../utils/feedback', () => ({ confirmAction: mocks.confirm }))
function render() { return mount(NodeKernelUpload, { props: { nodeId: 7, installedVersion: '0.0.3' } }) }
async function select(wrapper: ReturnType<typeof render>, file = new File(['kernel'], 'zero')) {
  wrapper.getComponent(UiFileUpload).vm.$emit('select', [file])
  await wrapper.get('input:not([type="file"])').setValue('0.0.2-rc.1')
}
describe('local kernel installation', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.confirm.mockResolvedValue(true); mocks.upload.mockResolvedValue({ id: 42 }) })
  it('confirms downgrade, uploads the selected file and hands off a durable task', async () => {
    const wrapper = render(); await select(wrapper)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ title: '确认降级 Zero 内核', tone: 'danger' }))
    expect(mocks.upload).toHaveBeenCalledWith(7, expect.any(File), '0.0.2-rc.1', true, expect.any(String), expect.any(Function))
    expect(wrapper.emitted('accepted')?.[0]).toEqual([{ id: 42 }, 7, '0.0.2-rc.1'])
    wrapper.unmount()
  })
  it('blocks double clicks while awaiting confirmation or upload and supports cancellation', async () => {
    let decide!: (value: boolean) => void
    mocks.confirm.mockImplementation(() => new Promise(resolve => { decide = resolve }))
    const wrapper = render(); await select(wrapper)
    await wrapper.get('button').trigger('click'); await wrapper.get('button').trigger('click')
    expect(mocks.confirm).toHaveBeenCalledTimes(1)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    decide(false); await flushPromises(); expect(mocks.upload).not.toHaveBeenCalled()
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined(); wrapper.unmount()
  })
  it('retains the idempotency key when a lost upload response is retried', async () => {
    mocks.upload.mockRejectedValueOnce(new Error('timeout'))
    const wrapper = render(); await select(wrapper)
    await wrapper.get('button').trigger('click'); await flushPromises()
    const key = mocks.upload.mock.calls[0][4]
    expect(wrapper.text()).toContain('上传结果未确认')
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(mocks.upload.mock.calls[1][4]).toBe(key); wrapper.unmount()
  })
  it('rejects oversized files and invalid versions without uploading', async () => {
    const wrapper = render()
    const file = new File(['x'], 'too-large'); Object.defineProperty(file, 'size', { value: 129 * 1048576 })
    await select(wrapper, file)
    expect(wrapper.text()).toContain('不超过 128 MB')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await select(wrapper); await wrapper.get('input:not([type="file"])').setValue('latest')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined(); expect(mocks.upload).not.toHaveBeenCalled(); wrapper.unmount()
  })
})
