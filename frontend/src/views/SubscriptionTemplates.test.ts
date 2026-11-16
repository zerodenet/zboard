import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import SubscriptionTemplates from './SubscriptionTemplates.vue'
import UiButton from '../components/UiButton.vue'
import UiInput from '../components/UiInput.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import UiTabs from '../components/UiTabs.vue'
import UiSelect from '../components/UiSelect.vue'
import AdminOrderLookup from '../components/AdminOrderLookup.vue'
import { fetchSubscriptionTemplate, fetchSubscriptionTemplatesPage, previewSubscriptionTemplate, updateSubscriptionTemplate, fetchSubscriptionsPage } from '../api/client'
import { defaultSubscriptionCustomization } from '../utils/subscriptionTemplateEditor'
vi.mock('../stores/app', () => ({ useAppStore: () => ({ token: 'fixture', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 }) }))
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchSubscriptionTemplate: vi.fn(), fetchSubscriptionTemplatesPage: vi.fn(), previewSubscriptionTemplate: vi.fn(), updateSubscriptionTemplate: vi.fn(), fetchSubscriptionsPage: vi.fn() }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(async () => true), notify: vi.fn() }))
let wrapper: VueWrapper | undefined
const item = () => ({ id: 1, revision: 3, name: 'Clash', slug: 'clash', description: '', renderer: 'clash' as const, customization: defaultSubscriptionCustomization('clash'), is_active: true, sort_order: 0, content_type: 'application/yaml' as const, created_at: '', updated_at: '' })
beforeEach(() => {
 vi.useFakeTimers(); vi.clearAllMocks(); vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
 vi.mocked(fetchSubscriptionTemplatesPage).mockResolvedValue({ items: [item()], total: 1, page: { offset: 0, limit: 50, total: 1, next_cursor: null, previous_cursor: null }, aggregates: {}, facets: {}, offset: 0, limit: 50 })
 const fromBackend = item(); delete (fromBackend.customization as Partial<typeof fromBackend.customization>).rule_sets
 vi.mocked(fetchSubscriptionTemplate).mockResolvedValue(fromBackend)
 vi.mocked(previewSubscriptionTemplate).mockResolvedValue({ content: 'proxy-groups: []\n', content_type: 'application/yaml', bytes: 17, line_count: 2, truncated: false, groups: [] })
 vi.mocked(updateSubscriptionTemplate).mockResolvedValue(item())
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.useRealTimers(); document.body.innerHTML = '' })
async function render() {
 const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/subscription-templates', component: SubscriptionTemplates }] })
 await router.push('/admin/subscription-templates?q=clash&template=1'); await router.isReady()
 wrapper = mount(SubscriptionTemplates, { attachTo: document.body, global: { plugins: [router], components: { UiButton, UiInput, UiCheckbox, PageRefreshButton }, stubs: { SubscriptionTemplateCustomizer: true, AdminOrderLookup: true } } })
 await flushPromises(); return router
}
async function previewTick() { await vi.advanceTimersByTimeAsync(450); await flushPromises() }
it('uses a full workspace and retains edits across mobile views without autosaving', async () => {
 await render(); expect(wrapper!.find('[role="dialog"]').exists()).toBe(false)
 await wrapper!.get('#template-name').setValue('Local edit'); await previewTick()
 expect(previewSubscriptionTemplate).toHaveBeenCalledOnce()
 expect(previewSubscriptionTemplate).toHaveBeenCalledWith(expect.objectContaining({ live: true, full_content: true }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
 const tabs = wrapper!.getComponent(UiTabs); tabs.vm.$emit('update:modelValue', 'preview'); await flushPromises()
 expect(wrapper!.get('.template-editor-grid').attributes('data-mobile-panel')).toBe('preview')
 tabs.vm.$emit('update:modelValue', 'edit'); await flushPromises()
 expect(wrapper!.get<HTMLInputElement>('#template-name').element.value).toBe('Local edit')
 expect(updateSubscriptionTemplate).not.toHaveBeenCalled()
})
it('saves independently of pending previews and preserves revision and list filters', async () => {
 vi.mocked(previewSubscriptionTemplate).mockImplementation(() => new Promise(() => {}))
 const router = await render(); await previewTick()
 await wrapper!.get('#template-name').setValue('Saved edit'); await wrapper!.get('form').trigger('submit'); await flushPromises()
 expect(updateSubscriptionTemplate).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Saved edit', expected_revision: 3 }))
 expect(router.currentRoute.value.query).toEqual({ q: 'clash' })
 expect(wrapper!.find('.template-workspace').exists()).toBe(false)
})
it('requires a real sample and reuses the bounded authorized subscription query', async () => {
 await render()
 const source = wrapper!.findAllComponents(UiSelect).find(select => (select.props('options') as Array<{ value: string }>).some(option => option.value === 'subscription'))!
 source.vm.$emit('update:modelValue', 'subscription'); await previewTick()
 expect(previewSubscriptionTemplate).not.toHaveBeenCalled()
 const lookup = wrapper!.getComponent(AdminOrderLookup)
 vi.mocked(fetchSubscriptionsPage).mockResolvedValue({ items: [], total: 0, page: { offset: 0, limit: 50, total: 1, next_cursor: null, previous_cursor: null }, aggregates: {}, facets: {}, offset: 0, limit: 25 })
 const signal = new AbortController().signal
 await lookup.props('fetchPage')('reader@example.test', 25, signal)
 expect(fetchSubscriptionsPage).toHaveBeenCalledWith({ q: 'reader@example.test', status: 'active', quota: 'available', offset: 25, limit: 25 }, { signal })
 lookup.vm.$emit('update:modelValue', { id: 17, label: 'Reader' }); await previewTick()
 expect(previewSubscriptionTemplate).toHaveBeenLastCalledWith(expect.objectContaining({ subscription_id: 17 }), expect.anything())
})
it('retains a conflicting draft and prevents overwriting a newer revision', async () => {
 vi.mocked(updateSubscriptionTemplate).mockRejectedValue({ response: { status: 409 } })
 await render(); await wrapper!.get('#template-name').setValue('Conflicting edit'); await wrapper!.get('form').trigger('submit'); await flushPromises()
 expect(wrapper!.text()).toContain('模板已在其他会话更新')
 expect(wrapper!.get<HTMLInputElement>('#template-name').element.value).toBe('Conflicting edit')
 await wrapper!.get('form').trigger('submit'); expect(updateSubscriptionTemplate).toHaveBeenCalledOnce()
})
