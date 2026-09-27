import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import ConfirmDialog from './ConfirmDialog.vue'

afterEach(() => { document.body.innerHTML = '' })

describe('ConfirmDialog', () => {
  it('requires an explicit action for destructive confirmation and blocks actions while busy', async () => {
    const wrapper = mount(ConfirmDialog, {
      attachTo: document.body,
      props: { open: true, title: '删除节点？', message: '此操作无法撤销。', tone: 'danger', confirmText: '删除节点' },
    })
    await flushPromises()
    const dialog = document.body.querySelector<HTMLElement>('[role="alertdialog"]')!
    expect(dialog.parentElement).toBe(document.body)
    expect(dialog.getAttribute('aria-labelledby')).toBeTruthy()
    expect(dialog.textContent).toContain('此操作无法撤销。')
    expect(dialog.textContent?.match(/此操作无法撤销。/g)).toHaveLength(1)
    dialog.querySelector<HTMLButtonElement>('button')!.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    const confirm = [...dialog.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent?.includes('删除节点'))!
    confirm.click()
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    await wrapper.setProps({ busy: true })
    expect(confirm.disabled).toBe(true)
    wrapper.unmount()
  })
})
