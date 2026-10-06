import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { SystemConfig } from '../api/client'
import SettingsConfigRow from './SettingsConfigRow.vue'
import UiNumberInput from './UiNumberInput.vue'

function config(overrides: Partial<SystemConfig>): SystemConfig {
  return {
    id: 1,
    config_key: 'smtp_port',
    name: 'SMTP 端口',
    value: 587,
    value_type: 'int',
    description: '邮件服务器端口',
    is_public: false,
    is_secret: false,
    configured: true,
    revision: 3,
    updated_at: '2026-07-23T00:00:00Z',
    input: { control: 'port', min: 1, max: 65535, step: 1 },
    ...overrides,
  }
}

describe('SettingsConfigRow', () => {
  it('edits a subscription alert switch and saves through the existing config action', async () => {
    const wrapper = mount(SettingsConfigRow, {
      props: {
        config: config({
          config_key: 'subscription_alert_low_enabled', name: '余量不足提醒',
          value: false, value_type: 'bool', input: { control: 'switch' },
        }),
        draft: false, dirty: true,
      },
    })
    expect(wrapper.text()).toContain('已关闭')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:draft')?.at(-1)).toEqual([true])
    await wrapper.setProps({ draft: true })
    expect(wrapper.text()).toContain('已启用')
    const save = wrapper.findAll('button').find(button => button.text().includes('保存'))!
    await save.trigger('click')
    expect(wrapper.emitted('save')).toHaveLength(1)
  })

  it('uses backend bounds for the shared subscriber alert interval', () => {
    const wrapper = mount(SettingsConfigRow, {
      props: {
        config: config({
          config_key: 'subscription_alert_interval_hours', name: '用户提醒间隔（小时）',
          value: 24, input: { control: 'integer', min: 6, max: 168, step: 1 },
        }),
        draft: 24,
      },
    })
    expect(wrapper.getComponent(UiNumberInput).props('min')).toBe(6)
    expect(wrapper.getComponent(UiNumberInput).props('max')).toBe(168)
  })

  it('renders the server-declared domain control with shared label and error semantics', () => {
    const wrapper = mount(SettingsConfigRow, {
      props: {
        config: config({}),
        draft: 70000,
        dirty: true,
        error: '数值不能大于 65535。',
      },
      global: { plugins: [] },
    })

    const input = wrapper.get('input')
    const numberInput = wrapper.getComponent(UiNumberInput)
    expect(wrapper.text()).toContain('端口')
    expect(numberInput.props('min')).toBe(1)
    expect(numberInput.props('max')).toBe(65535)
    expect(input.attributes('aria-labelledby')).toBe('smtp_port-label')
    expect(input.attributes('aria-describedby')).toContain('smtp_port-description')
    expect(input.attributes('aria-describedby')).toContain('smtp_port-error')
    expect(input.attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('[role="alert"]').text()).toContain('65535')
  })

  it('renders declared options instead of exposing the raw value type', () => {
    const wrapper = mount(SettingsConfigRow, {
      props: {
        config: config({
          config_key: 'smtp_tls_mode',
          name: 'SMTP TLS 模式',
          value: 'starttls',
          value_type: 'string',
          input: {
            control: 'select',
            options: [
              { label: 'STARTTLS（推荐）', value: 'starttls' },
              { label: '隐式 TLS', value: 'implicit' },
            ],
          },
        }),
        draft: 'starttls',
      },
      global: { plugins: [] },
    })

    expect(wrapper.text()).toContain('选项')
    expect(wrapper.get('[role="combobox"]').attributes('aria-labelledby')).toBe('smtp_tls_mode-label')
    expect(wrapper.text()).not.toContain('string')
  })

  it('keeps text and JSON drafts controlled through the shared wrappers', async () => {
    const hostname = mount(SettingsConfigRow, {
      props: {
        config: config({
          config_key: 'smtp_host',
          name: 'SMTP host',
          value: 'smtp.example.com',
          value_type: 'string',
          input: { control: 'hostname' },
        }),
        draft: 'smtp.example.com',
      },
      global: { plugins: [] },
    })
    expect(hostname.get('input').element.value).toBe('smtp.example.com')
    await hostname.get('input').setValue('mail.example.com')
    expect(hostname.emitted('update:draft')?.at(-1)).toEqual(['mail.example.com'])

    const json = mount(SettingsConfigRow, {
      props: {
        config: config({
          config_key: 'advanced_delivery_rules',
          name: 'Delivery rules',
          value: { retry: 3 },
          value_type: 'json',
          input: { control: 'json' },
        }),
        draft: '{\n  "retry": 3\n}',
      },
      global: { plugins: [] },
    })
    expect(json.get('textarea').element.value).toContain('"retry": 3')
    await json.get('textarea').setValue('{"retry":5}')
    expect(json.emitted('update:draft')?.at(-1)).toEqual(['{"retry":5}'])
  })
})
