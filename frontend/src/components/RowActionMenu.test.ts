import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import RowActionMenu from './RowActionMenu.vue'

afterEach(() => {
  document.body.innerHTML = ''
})

describe('RowActionMenu', () => {
  it('opens an accessible popup and returns focus after Escape', async () => {
    const wrapper = mount(RowActionMenu, {
      attachTo: document.body,
      slots: { default: '<button role="menuitem">查看详情</button><button role="menuitem">编辑</button>' },
    })
    const trigger = wrapper.get('button')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    const menu = document.body.querySelector<HTMLElement>('[role="menu"]')
    expect(menu).not.toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.activeElement?.textContent).toContain('查看详情')

    menu?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })
})
