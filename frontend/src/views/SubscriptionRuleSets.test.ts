import PrimeVue from 'primevue/config'
import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SubscriptionRuleSets from './SubscriptionRuleSets.vue'

const mocks = vi.hoisted(() => ({ create: vi.fn(), list: vi.fn(), confirm: vi.fn() }))
vi.mock('../api/managedRuleSets', () => ({
  createManagedRuleSet: mocks.create, fetchManagedRuleSetsPage: mocks.list,
  deleteManagedRuleSet: vi.fn(), fetchManagedRuleSet: vi.fn(), fetchManagedRuleSetContent: vi.fn(),
  importManagedRuleSet: vi.fn(), updateManagedRuleSet: vi.fn(),
}))
vi.mock('../utils/feedback', () => ({ confirmAction: mocks.confirm, notify: vi.fn() }))
const modal = defineComponent({
  props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>',
})
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.resetAllMocks()
  mocks.list.mockResolvedValue({ items: [], total: 0 })
  mocks.confirm.mockResolvedValue(false)
  mocks.create.mockResolvedValue({ id: 1 })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: SubscriptionRuleSets },
    { path: '/next', component: { template: '<div>Next page</div>' } },
    { path: '/admin/subscription-templates', component: { template: '<div />' } },
    { path: '/admin/subscription-templates/rule-sets', component: { template: '<div />' } },
  ] })
  await router.push('/'); await router.isReady()
  wrapper = mount(defineComponent({ components: { RouterView }, template: '<RouterView />' }), {
    attachTo: document.body,
    global: { plugins: [router, PrimeVue], stubs: { ModalDialog: modal } },
  })
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text().includes('新建规则集'))!.trigger('click')
  return router
}
async function fillRequired() {
  await wrapper!.get('#managed-rule-set-name').setValue('Example')
  await wrapper!.get('#managed-rule-set-tag').setValue('example')
}

describe('managed rule set editor', () => {
  it('protects dirty drafts on navigation and browser unload, and allows explicit discard', async () => {
    const router = await render()
    await fillRequired()
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)
    await router.push('/next')
    expect(router.currentRoute.value.path).toBe('/')
    expect(wrapper!.get<HTMLInputElement>('#managed-rule-set-name').element.value).toBe('Example')
    mocks.confirm.mockResolvedValueOnce(true)
    await router.push('/next')
    expect(router.currentRoute.value.path).toBe('/next')
  })

  it('focuses invalid input and clears its error as the user corrects it', async () => {
    await render()
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    const name = wrapper!.get('#managed-rule-set-name')
    expect(name.attributes('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(name.element)
    expect(mocks.create).not.toHaveBeenCalled()
    await name.setValue('Corrected')
    expect(name.attributes('aria-invalid')).toBeUndefined()
  })

  it('maps versioned backend field errors to the form and retains the draft', async () => {
    await render()
    await fillRequired()
    mocks.create.mockRejectedValueOnce({ response: { status: 400, data: {
      message: '规则集信息校验失败。',
      error: { version: 1, code: 'validation_failed', fields: { tag: '该规则集标识已存在。' } },
    } } })
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    const tag = wrapper!.get('#managed-rule-set-tag')
    expect(tag.attributes('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(tag.element)
    expect(wrapper!.text()).toContain('该规则集标识已存在。')
    expect(wrapper!.get<HTMLInputElement>('#managed-rule-set-name').element.value).toBe('Example')
    await tag.setValue('another')
    expect(tag.attributes('aria-invalid')).toBeUndefined()
  })

  it('closes a successfully saved draft without prompting on later navigation', async () => {
    const router = await render()
    await fillRequired()
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledOnce()
    expect(wrapper!.find('form').exists()).toBe(false)
    await router.push('/next')
    expect(router.currentRoute.value.path).toBe('/next')
    expect(mocks.confirm).not.toHaveBeenCalled()
  })
})
