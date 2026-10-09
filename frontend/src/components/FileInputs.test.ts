import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SiteImageInput from './SiteImageInput.vue'
import TicketAttachmentEditor from './TicketAttachmentEditor.vue'
import TicketAttachments from './TicketAttachments.vue'
import SettingsPublicField from './SettingsPublicField.vue'
import UiFileUpload from './UiFileUpload.vue'
import UiInput from './UiInput.vue'
import { deleteFile, downloadFile, uploadFile } from '../api/files'

vi.mock('../api/files', () => ({ uploadFile: vi.fn(), deleteFile: vi.fn(async () => undefined), downloadFile: vi.fn(async () => undefined) }))

describe('local file and URL inputs', () => {
  beforeEach(() => vi.clearAllMocks())
  it('fills the site image draft after upload while retaining external URL input', async () => {
    vi.mocked(uploadFile).mockResolvedValue({ id: 'image-id', name: 'logo.svg', purpose: 'site', size: 10, content_type: 'image/svg+xml', url: '/media/image-id' })
    const wrapper = mount(SiteImageInput, { props: { modelValue: 'https://cdn.example/logo.svg' } })
    expect(wrapper.get('input[type="text"]').element).toHaveProperty('value', 'https://cdn.example/logo.svg')
    const file = new File(['<svg/>'], 'logo.svg')
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [file])
    await flushPromises()
    expect(uploadFile).toHaveBeenCalledWith(file, 'site', expect.any(Function))
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['/media/image-id'])
    expect(wrapper.text()).toContain('上传后需保存设置')
    wrapper.unmount()
  })
  it('shows size and upload failures without replacing the previous image', async () => {
    const wrapper = mount(SiteImageInput, { props: { modelValue: 'https://cdn.example/old.png' } })
    const big = new File(['x'], 'big.png'); Object.defineProperty(big, 'size', { value: 2 * 1024 * 1024 + 1 })
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [big]); await flushPromises()
    expect(uploadFile).not.toHaveBeenCalled(); expect(wrapper.text()).toContain('不得超过 2 MiB')
    vi.mocked(uploadFile).mockRejectedValue(new Error('upload failed'))
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [new File(['x'], 'small.png')]); await flushPromises()
    expect(wrapper.text()).toContain('图片上传失败')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })
  it('exposes uploads only on the three site image fields', () => {
    const config = { id: 1, config_key: 'site_logo_dark', name: '深色 Logo', value_type: 'string', is_secret: false, value: '', description: '', revision: 1, is_public: true }
    const wrapper = mount(SettingsPublicField, { props: { config: config as any, draft: '' } })
    expect(wrapper.findComponent(SiteImageInput).exists()).toBe(true)
    wrapper.unmount()
    const other = mount(SettingsPublicField, { props: { config: { ...config, config_key: 'site_support_url' } as any, draft: '' } })
    expect(other.findComponent(SiteImageInput).exists()).toBe(false)
    other.unmount()
  })
  it('adds a private file, removes it, and accepts a validated URL', async () => {
    vi.mocked(uploadFile).mockResolvedValue({ id: 'file-id', name: 'trace.log', purpose: 'ticket', size: 10, content_type: 'text/plain', url: '/api/v1/files/file-id' })
    const wrapper = mount(TicketAttachmentEditor, { props: { modelValue: [] } })
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [new File(['trace'], 'trace.log')]); await flushPromises()
    const items = wrapper.emitted('update:modelValue')![0][0] as any[]
    expect(items).toEqual([{ file_id: 'file-id', name: 'trace.log', size: 10, content_type: 'text/plain' }])
    expect(wrapper.emitted('update:busy')).toEqual([[true], [false]])
    await wrapper.setProps({ modelValue: items })
    await wrapper.findAll('button').find(item => item.text() === '移除')!.trigger('click'); await flushPromises()
    expect(deleteFile).toHaveBeenCalledWith('file-id')
    await wrapper.setProps({ modelValue: [] })
    wrapper.getComponent(UiInput).vm.$emit('update:modelValue', 'javascript:alert(1)'); await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === '添加链接')!.trigger('click')
    expect(wrapper.text()).toContain('请输入完整 HTTP/HTTPS')
    wrapper.getComponent(UiInput).vm.$emit('update:modelValue', 'https://files.example/trace.log'); await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === '添加链接')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([{ name: 'trace.log', url: 'https://files.example/trace.log' }])
    wrapper.unmount()
  })
  it('cleans up an upload that finishes after the editor was discarded', async () => {
    let finish!: (value: any) => void
    vi.mocked(uploadFile).mockReturnValue(new Promise(resolve => finish = resolve))
    const wrapper = mount(TicketAttachmentEditor, { props: { modelValue: [] } })
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [new File(['trace'], 'trace.log')])
    wrapper.unmount()
    finish({ id: 'late-file', name: 'trace.log', size: 10 })
    await flushPromises()
    expect(deleteFile).toHaveBeenCalledWith('late-file')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it('downloads private attachments through the authenticated API and opens only external links', async () => {
    const wrapper = mount(TicketAttachments, { props: { items: [{ file_id: 'private-file', name: 'private.log', url: '/api/v1/files/private-file' }, { name: 'external.log', url: 'https://files.example/external.log' }] } })
    expect(wrapper.findAll('a')).toHaveLength(1)
    expect(wrapper.get('a').attributes('rel')).toContain('noopener')
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(downloadFile).toHaveBeenCalledWith('private-file', 'private.log')
    wrapper.unmount()
  })
})
