import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { describe, expect, it } from 'vitest'
import { defaultSubscriptionCustomization } from '../utils/subscriptionTemplateEditor'
import SubscriptionTemplateCustomizer from './SubscriptionTemplateCustomizer.vue'
import UiSelect from './UiSelect.vue'
import { subscriptionPolicyGroupDefaultTarget, setSubscriptionPolicyGroupSpecialTarget } from '../utils/subscriptionTemplateEditor'

describe('SubscriptionTemplateCustomizer', () => {
  it('offers a default even when no other policy group exists', () => {
    const customization = defaultSubscriptionCustomization('clash')
    customization.policy_groups = [{ id: 'main', name: '支付服务', type: 'select', include_groups: [] }]
    const wrapper = mount(SubscriptionTemplateCustomizer, { props: { renderer: 'clash', modelValue: customization } })
    const picker = wrapper.findAllComponents(UiSelect).find(select => (select.props('options') || []).some(option => option.label === '第一个成员'))!
    expect((picker.props('options') || []).some(option => option.value === 'direct')).toBe(true)
    wrapper.unmount()
  })
  it.each(['clash', 'sing-box', 'znet-sink'] as const)('selects DIRECT as the default member for %s and clears it when excluded', async renderer => {
    const customization = reactive(defaultSubscriptionCustomization(renderer))
    const wrapper = mount(SubscriptionTemplateCustomizer, { props: { renderer, modelValue: customization } })
    const main = customization.policy_groups[0]
    const picker = wrapper.findAllComponents(UiSelect).find(select => (select.props('options') || []).some(option => option.label === '第一个成员'))!
    expect(picker).toBeDefined()
    expect(picker.props('options')).toEqual(expect.arrayContaining([{ label: 'DIRECT · 直连', value: 'direct' }]))
    if (renderer === 'sing-box') expect((picker.props('options') || []).some(option => option.value === 'reject')).toBe(false)
    picker.vm.$emit('update:modelValue', 'direct')
    await wrapper.vm.$nextTick()
    expect(subscriptionPolicyGroupDefaultTarget(main)).toBe('direct')
    expect(main.default_group).toBe('')
    setSubscriptionPolicyGroupSpecialTarget(main, 'direct', false)
    await wrapper.vm.$nextTick()
    expect(subscriptionPolicyGroupDefaultTarget(main)).toBe('')
    expect((picker.props('options') || []).some(option => option.value === 'direct')).toBe(false)
    wrapper.unmount()
  })
  it('hides the desktop proxy setter for TUN clients even when an older template enabled it', async () => {
    const customization = defaultSubscriptionCustomization('sing-box')
    customization.system_proxy = true
    const wrapper = mount(SubscriptionTemplateCustomizer, {
      props: { renderer: 'sing-box', modelValue: customization },
      global: { plugins: [] },
    })
    expect(wrapper.text()).toContain('自动设置系统 HTTP 代理（桌面命令行）')
    const enable = wrapper.findAll('button').find(button => button.text() === '启用客户端 HTTP 代理开关')
    await enable!.trigger('click')
    expect(customization.tun.enabled).toBe(true)
    expect(customization.system_proxy).toBe(true)
    expect(wrapper.get('[role="status"]').text()).toBe('客户端 HTTP 代理已启用')
    expect(wrapper.text()).not.toContain('自动设置系统 HTTP 代理（桌面命令行）')
    const tun = wrapper.findAll('label.setting-switch').find(label => label.text().includes('启用 TUN 全局接管'))!
    await tun.find('[role="switch"]').trigger('click')
    expect(wrapper.text()).toContain('自动设置系统 HTTP 代理（桌面命令行）')
    expect(customization.system_proxy).toBe(true)
  })

  it('prepares the sing-box client HTTP switch with TUN, mixed inbound and DNS', async () => {
    const customization = defaultSubscriptionCustomization('sing-box')
    const wrapper = mount(SubscriptionTemplateCustomizer, {
      props: { renderer: 'sing-box', modelValue: customization },
      global: { plugins: [] },
    })
    expect(customization.mode).toBe('rule')
    const enable = wrapper.findAll('button').find(button => button.text() === '启用客户端 HTTP 代理开关')
    expect(enable).toBeDefined()
    await enable!.trigger('click')
    expect(customization.mixed_enabled).toBe(true)
    expect(customization.tun.enabled).toBe(true)
    expect(customization.dns.enabled).toBe(true)
    expect(customization.system_proxy).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toBe('客户端 HTTP 代理已启用')
    expect(wrapper.findAll('button').some(button => button.text() === '启用客户端 HTTP 代理开关')).toBe(false)
  })

  it('edits native policy groups and gates validated direct configuration behind the advanced tab', async () => {
    const customization = defaultSubscriptionCustomization('clash')
    let rawUpdate = customization
    const wrapper = mount(SubscriptionTemplateCustomizer, {
      props: { renderer: 'clash', modelValue: customization, 'onUpdate:modelValue': value => { rawUpdate = value } },
      global: { plugins: [] },
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
    await advanced?.trigger('mousedown', { button: 0, ctrlKey: false })
    expect(wrapper.find('.template-code-editor').exists()).toBe(true)
    expect(wrapper.text()).toContain('proxies')
    expect(wrapper.text()).toContain('$zboard:generated-proxies')

    const raw = wrapper.findAll('[role="tab"]').find(tab => tab.text().includes('Raw 模型'))
    await raw?.trigger('mousedown', { button: 0, ctrlKey: false })
    expect(wrapper.text()).toContain('模板 JSON')
    const rawEditor = wrapper.find('[aria-label="Raw 订阅模板 JSON"]')
    expect(rawEditor.exists()).toBe(true)
    await rawEditor.setValue(JSON.stringify({ ...customization, mode: 'global', system_proxy: false }))
    expect(rawUpdate.mode).toBe('global')
  })
})
