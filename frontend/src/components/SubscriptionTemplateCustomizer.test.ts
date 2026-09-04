import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defaultSubscriptionCustomization } from '../utils/subscriptionTemplateEditor'
import SubscriptionTemplateCustomizer from './SubscriptionTemplateCustomizer.vue'

describe('SubscriptionTemplateCustomizer', () => {
  it('edits native policy groups and gates validated direct configuration behind the advanced tab', async () => {
    const customization = defaultSubscriptionCustomization('clash')
    let rawUpdate = customization
    const wrapper = mount(SubscriptionTemplateCustomizer, {
      props: { renderer: 'clash', modelValue: customization, 'onUpdate:modelValue': value => { rawUpdate = value } },
      global: { plugins: [PrimeVue] },
    })

    expect(wrapper.text()).not.toContain('标准分流')
    expect(wrapper.text()).toContain('初始运行模式')
    expect(wrapper.text()).toContain('本地混合代理端口')
    expect(wrapper.text()).toContain('启用本地 HTTP/SOCKS 代理')
    expect(wrapper.text()).toContain('启用 DNS')
    expect(wrapper.text()).toContain('启用 TUN 全局接管')
    expect(customization.mixed_enabled).toBe(true)
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

    const raw = wrapper.findAll('[role="tab"]').find(tab => tab.text().includes('Raw 模型'))
    await raw?.trigger('click')
    expect(wrapper.text()).toContain('Raw 模型与可视化双向同步')
    const rawEditor = wrapper.find('[aria-label="Raw 订阅模板 JSON"]')
    expect(rawEditor.exists()).toBe(true)
    await rawEditor.setValue(JSON.stringify({ ...customization, mode: 'global', system_proxy: false }))
    expect(rawUpdate.mode).toBe('global')
  })
})
