import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import RowActionMenu from './RowActionMenu.vue'
import UiButton from './UiButton.vue'
import ModalDialog from './ModalDialog.vue'
import DetailDrawer from './DetailDrawer.vue'
import { defineComponent, ref } from 'vue'

afterEach(() => {
  document.body.innerHTML = ''
})

describe('RowActionMenu', () => {
  it('opens an action dialog without stealing its focus when the menu closes', async () => {
    const fixture = defineComponent({
      components: { RowActionMenu, UiButton, ModalDialog, DetailDrawer },
      setup() { return { open: ref(false) } },
      template: '<DetailDrawer :open="true" title="节点"><RowActionMenu><UiButton @click="open = true">编辑连接</UiButton></RowActionMenu></DetailDrawer><ModalDialog :open="open" title="编辑连接" @close="open = false"><input aria-label="连接名称" /></ModalDialog>',
    })
    const wrapper = mount(fixture, { attachTo: document.body })
    await flushPromises()
    document.body.querySelector<HTMLElement>('[aria-label="更多操作"]')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await flushPromises()
    const item = document.body.querySelector<HTMLElement>('[role="menuitem"]')!
    item.click()
    await flushPromises()
    expect(document.body.querySelector('.app-dialog')).not.toBeNull()
    expect(document.activeElement?.closest('.app-dialog')).not.toBeNull()
    wrapper.unmount()
  })
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
    await flushPromises()
    expect(document.body.querySelector('[role="menu"]')?.getAttribute('data-state') ?? 'closed').toBe('closed')
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })
})
