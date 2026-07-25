import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import DetailDrawer from './DetailDrawer.vue'

const UiButtonStub = defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) },
})

afterEach(() => { document.body.innerHTML = '' })

describe('DetailDrawer', () => {
  it('traps keyboard focus, closes on Escape and restores the opener focus', async () => {
    const opener = document.createElement('button')
    opener.textContent = '打开详情'
    document.body.appendChild(opener)
    opener.focus()

    const wrapper = mount(DetailDrawer, {
      attachTo: document.body,
      props: { open: false, title: '节点详情', description: '节点状态' },
      slots: { default: '<button id="drawer-action">执行操作</button>' },
      global: { stubs: { UiButton: UiButtonStub, UiIcon: true } },
    })

    await wrapper.setProps({ open: true })
    await nextTick()
    const drawer = document.body.querySelector<HTMLElement>('.detail-drawer')!
    const close = document.body.querySelector<HTMLButtonElement>('.detail-drawer-header [aria-label="关闭详情"]')!
    const action = document.body.querySelector<HTMLButtonElement>('#drawer-action')!
    expect(document.activeElement).toBe(close)
    expect(drawer.getAttribute('aria-describedby')).toBeTruthy()

    action.focus()
    drawer.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }))
    expect(document.activeElement).toBe(close)
    close.focus()
    drawer.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true }))
    expect(document.activeElement).toBe(action)

    drawer.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.setProps({ open: false })
    await nextTick()
    expect(document.activeElement).toBe(opener)
    wrapper.unmount()
  })
})
