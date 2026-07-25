import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { feedbackState, settleConfirm } from '../utils/feedback'
import ModalDialog from './ModalDialog.vue'

afterEach(() => {
  while (feedbackState.confirm) settleConfirm(false)
  document.body.innerHTML = ''
})

describe('ModalDialog', () => {
  it('uses the shared confirmation flow before closing a dirty form', async () => {
    const wrapper = mount(ModalDialog, {
      props: { open: true, title: '编辑节点', dirty: true },
      global: { plugins: [PrimeVue] },
    })

    const closing = (wrapper.vm as unknown as { requestClose: () => Promise<void> }).requestClose()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(feedbackState.confirm?.title).toBe('放弃未保存的修改？')

    settleConfirm(true)
    await closing
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('returns focus to the declared trigger after URL-driven closure', async () => {
    const trigger = document.createElement('button')
    trigger.dataset.modalTrigger = '7'
    document.body.appendChild(trigger)
    trigger.focus()
    const wrapper = mount(ModalDialog, {
      props: { open: false, title: '编辑模板', returnFocusSelector: '[data-modal-trigger="7"]' },
      global: { plugins: [PrimeVue] },
    })

    await wrapper.setProps({ open: true })
    await wrapper.setProps({ open: false })
    ;(wrapper.vm as unknown as { restoreFocus: () => void }).restoreFocus()
    expect(document.activeElement).toBe(trigger)
  })
})
