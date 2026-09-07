import PrimeVue from 'primevue/config'
import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SubscriptionRuleSets from './SubscriptionRuleSets.vue'
import UiSelect from '../components/UiSelect.vue'

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
  it('defaults remote imports to auto detection and explains process compatibility', async () => {
    await render()
    expect(wrapper!.text()).toContain('不能绑定 Zero 模板')
    const mode = wrapper!.findAllComponents(UiSelect).find(select =>
      (select.props('options') as Array<{ value: string }>).some(option => option.value === 'remote'))!
    mode.vm.$emit('update:modelValue', 'remote')
    await flushPromises()
    const format = wrapper!.findAllComponents(UiSelect).find(select =>
      (select.props('options') as Array<{ value: string }>).some(option => option.value === 'clash_classical'))!
    expect(format.props('modelValue')).toBe('auto')
    expect(wrapper!.text()).toContain('包含进程规则时会完整保留')
  })

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

  it('explains source formats and lets a mismatched AdBlock import be corrected in place', async () => {
    await render()
    await fillRequired()
    const selectOption = async (value: string) => {
      const select = wrapper!.findAllComponents(UiSelect).find(item =>
        (item.props('options') as Array<{ value: string }>).some(option => option.value === value))!
      select.vm.$emit('update:modelValue', value)
      await flushPromises()
    }
    await selectOption('remote')
    await selectOption('cidr_list')
    expect(wrapper!.text()).toContain('只接受纯 IP 网段')
    const url = 'https://raw.githubusercontent.com/dler-io/Rules/refs/heads/main/Clash/Provider/AdBlock.yaml'
    await wrapper!.get('#managed-rule-set-source-url').setValue(url)
    mocks.create.mockRejectedValueOnce({ response: { status: 400, data: {
      message: '远端规则导入失败。',
      error: { version: 1, code: 'validation_failed', fields: {
        source_format: 'payload 第 1 项：请将“远端来源格式”改为“Clash classical”后重新导入。',
      } },
    } } })
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper!.text()).toContain('请将“远端来源格式”改为“Clash classical”')
    expect(wrapper!.get<HTMLInputElement>('#managed-rule-set-source-url').element.value).toBe(url)
    await selectOption('clash_classical')
    expect(wrapper!.text()).toContain('payload 包装的 Clash Provider YAML')
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenLastCalledWith(expect.objectContaining({ source_url: url, source_format: 'clash_classical' }))
    expect(wrapper!.find('form').exists()).toBe(false)
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
