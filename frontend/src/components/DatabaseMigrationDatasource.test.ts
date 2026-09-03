import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DatabaseMigrationDatasource from './DatabaseMigrationDatasource.vue'

function mountField(driver: 'mysql' | 'sqlite', value = '') {
  const wrapper = mount(DatabaseMigrationDatasource, {
    props: {
      driver,
      modelValue: value,
      'onUpdate:modelValue': (next: string) => { void wrapper.setProps({ modelValue: next }) },
    },
    global: { plugins: [PrimeVue] },
  })
  return wrapper
}

describe('DatabaseMigrationDatasource', () => {
  it('shows the SQLite server file path as editable plaintext, not a password', async () => {
    const wrapper = mountField('sqlite')
    const input = wrapper.get<HTMLInputElement>('#migration-dsn')
    expect(wrapper.get('label').text()).toBe('目标 SQLite 文件路径')
    expect(input.attributes('type')).toBe('text')
    expect(input.attributes('autocomplete')).toBe('off')
    expect(input.attributes('aria-describedby')).toBe('migration-dsn-message')
    expect(wrapper.text()).toContain('不是本机路径')
    expect(wrapper.find('button').exists()).toBe(false)
    await input.setValue('  /data/migration-test.db  ')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['/data/migration-test.db'])
    wrapper.unmount()
  })

  it('conceals MySQL DSNs by default and toggles display without changing the value', async () => {
    const dsn = 'fixture:example-only@tcp(db:3306)/fixture?parseTime=true'
    const wrapper = mountField('mysql', dsn)
    const input = wrapper.get<HTMLInputElement>('#migration-dsn')
    expect(wrapper.get('label').text()).toBe('目标 MySQL 连接串')
    expect(input.attributes('type')).toBe('password')
    expect(input.attributes('autocomplete')).toBe('new-password')
    const toggle = wrapper.get('button')
    expect(toggle.attributes('type')).toBe('button')
    expect(toggle.attributes('aria-controls')).toBe('migration-dsn')
    expect(toggle.attributes('aria-pressed')).toBe('false')
    await toggle.trigger('click')
    expect(input.attributes('type')).toBe('text')
    expect(input.element.value).toBe(dsn)
    expect(toggle.attributes('aria-label')).toBe('隐藏 MySQL 连接串')
    await toggle.trigger('click')
    expect(input.attributes('type')).toBe('password')
    expect(input.element.value).toBe(dsn)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('clears the previous DSN and resets visibility when the target driver changes', async () => {
    const wrapper = mountField('mysql', 'fixture:example-only@tcp(db:3306)/fixture')
    await wrapper.get('button').trigger('click')
    await wrapper.setProps({ driver: 'sqlite' })
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('')
    expect(wrapper.get('input').attributes('type')).toBe('text')
    expect(wrapper.find('button').exists()).toBe(false)
    await wrapper.setProps({ driver: 'mysql' })
    expect(wrapper.get('input').attributes('type')).toBe('password')
    expect(wrapper.get('button').attributes('aria-pressed')).toBe('false')
    wrapper.unmount()
  })
})
