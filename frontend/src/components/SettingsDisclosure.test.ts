import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SettingsDisclosure from './SettingsDisclosure.vue'

describe('settings task disclosure', () => {
  it('keeps an edited field mounted when folded and exposes unsaved changes', async () => {
    const wrapper = mount(SettingsDisclosure, { props: { title: '视觉品牌', dirty: true }, slots: { default: '<input aria-label="Logo" value="draft.svg" />' } })
    const input = wrapper.get<HTMLInputElement>('input')
    await input.setValue('edited.svg')
    const field = input.element
    wrapper.get<HTMLDetailsElement>('details').element.open = true
    wrapper.get<HTMLDetailsElement>('details').element.open = false
    expect(wrapper.get('input').element).toBe(field)
    expect(field.value).toBe('edited.svg')
    expect(wrapper.find('[aria-label="有未保存修改"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('opens the affected group on validation failure and prevents folding during save', async () => {
    const wrapper = mount(SettingsDisclosure, { props: { title: '视觉品牌' } })
    expect(wrapper.get<HTMLDetailsElement>('details').element.open).toBe(false)
    await wrapper.setProps({ error: true, busy: true })
    await flushPromises()
    expect(wrapper.get<HTMLDetailsElement>('details').element.open).toBe(true)
    const click = new MouseEvent('click', { cancelable: true, bubbles: true })
    wrapper.get('summary').element.dispatchEvent(click)
    expect(click.defaultPrevented).toBe(true)
    wrapper.unmount()
  })
})
