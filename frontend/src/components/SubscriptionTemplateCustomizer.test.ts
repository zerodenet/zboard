import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defaultSubscriptionCustomization } from '../utils/subscriptionTemplateEditor'
import SubscriptionTemplateCustomizer from './SubscriptionTemplateCustomizer.vue'

describe('SubscriptionTemplateCustomizer', () => {
  it('edits native policy groups and gates validated direct configuration behind the advanced tab', async () => {
    const customization = defaultSubscriptionCustomization('clash')
    const wrapper = mount(SubscriptionTemplateCustomizer, {
      props: { renderer: 'clash', modelValue: customization },
      global: { plugins: [PrimeVue] },
    })

    expect(wrapper.text()).not.toContain('标准分流')
    expect(wrapper.text()).toContain('本地混合入站端口')
    expect(customization.mixed_port).toBe(7890)
    expect(wrapper.text()).toContain('策略组名称')
    expect(wrapper.text()).toContain('自动测速')
    expect(wrapper.text()).toContain('包含节点名称')
    expect(wrapper.text()).toContain('尚未绑定规则集')
    expect(wrapper.find('.template-code-editor').exists()).toBe(false)

    const add = wrapper.findAll('button').find(button => button.text().includes('快捷添加远端'))
    await add?.trigger('click')
    expect(customization.rule_sets).toHaveLength(1)
    expect(customization.rule_sets[0].target).toBe('group:main')
    expect(wrapper.text()).toContain('未命名远端规则集')

    const advanced = wrapper.findAll('[role="tab"]').find(tab => tab.text().includes('高级配置'))
    await advanced?.trigger('click')
    expect(wrapper.find('.template-code-editor').exists()).toBe(true)
    expect(wrapper.text()).toContain('proxies')
    expect(wrapper.text()).toContain('$zboard:generated-proxies')
  })
})
