import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SubscriptionTemplatePreview from './SubscriptionTemplatePreview.vue'
import UiTabs from './UiTabs.vue'
import UiSelect from './UiSelect.vue'
import type { SubscriptionTemplatePreview as Preview } from '../api/client'
const content = '  {"支付":"e\u0301"}\r\n\r\n'
const preview: Preview = { content, content_type: 'application/json', bytes: 26, line_count: 2, truncated: false, groups: [{ name: '支付服务', type: 'selector', members: ['direct', '节点选择'], default: 'direct' }, { name: '节点选择', type: 'selector', members: ['Tokyo'], default: 'Tokyo' }], delivery_encoding: 'base64', delivery_content_type: 'text/plain' }
afterEach(() => { vi.restoreAllMocks(); localStorage.clear(); document.body.innerHTML = '' })
describe('subscription preview effects and Raw', () => {
  it('uses final output groups and follows the focused editor group', async () => {
    const wrapper = mount(SubscriptionTemplatePreview, { props: { preview, loading: false, stale: false, error: '', focusGroup: '支付服务' } })
    expect(wrapper.get('.subscription-preview-members .selected').text()).toBe('direct')
    await wrapper.setProps({ focusGroup: '节点选择' })
    expect(wrapper.get('.subscription-preview-members .selected').text()).toBe('Tokyo')
    wrapper.unmount()
  })
  it('copies exact Raw and offers the actual Base64 delivery without asking for another render', async () => {
    const writeText = vi.fn(async () => undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const wrapper = mount(SubscriptionTemplatePreview, { props: { preview, loading: false, stale: false, error: '' } })
    wrapper.getComponent(UiTabs).vm.$emit('update:modelValue', 'raw'); await flushPromises()
    const copy = () => wrapper.findAll('button').find(button => button.text() === '复制')!
    await copy().trigger('click'); expect(writeText).toHaveBeenCalledWith(content)
    wrapper.findAllComponents(UiSelect).find(select => (select.props('options') || []).some(option => option.value === 'delivery'))!.vm.$emit('update:modelValue', 'delivery')
    await flushPromises(); await copy().trigger('click')
    expect(writeText).toHaveBeenLastCalledWith(Buffer.from(content, 'utf8').toString('base64'))
    expect(wrapper.emitted('retry')).toBeUndefined()
    expect(localStorage.getItem('zboard.template-preview.view')).toBe('raw')
    await wrapper.setProps({ preview: { ...preview, content: 'proxies: []\n', content_type: 'application/yaml', delivery_encoding: 'identity' } })
    expect(wrapper.get('textarea').element.value).toBe('proxies: []\n')
    expect(wrapper.get('.subscription-preview-raw-toolbar').text()).toContain('YAML')
    wrapper.unmount()
  })
  it('keeps stale Raw readable while preventing it being mistaken for the latest export', async () => {
    localStorage.setItem('zboard.template-preview.view', 'raw')
    const wrapper = mount(SubscriptionTemplatePreview, { props: { preview, loading: false, stale: true, error: '连接失败' } })
    expect(wrapper.get('textarea').element.value).toContain('支付')
    expect(wrapper.findAll('button').find(button => button.text() === '复制')!.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('上次结果')
    wrapper.unmount()
  })
})
