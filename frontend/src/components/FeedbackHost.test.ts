import { nextTick } from 'vue'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { dismissToast, feedbackState, notify } from '../utils/feedback'
import { primeVueOptions } from '../theme/primevue'
import FeedbackHost from './FeedbackHost.vue'
import UiButton from './UiButton.vue'

afterEach(() => {
  for (const item of [...feedbackState.toasts]) dismissToast(item.id)
  document.body.innerHTML = ''
})

describe('FeedbackHost', () => {
  it('renders the deduplicated three-toast cap and synchronizes manual dismissal', async () => {
    const wrapper = mount(FeedbackHost, {
      attachTo: document.body,
      global: {
        plugins: [[PrimeVue, primeVueOptions], ToastService],
        components: { UiButton },
      },
    })

    notify('第一条', '会被容量淘汰。', 'success')
    notify('第一条', '会被容量淘汰。', 'success')
    notify('第二条', '节点已更新。', 'success')
    notify('第三条', '订单已更新。', 'success')
    notify('第四条', '设置已保存。', 'success')
    await nextTick()
    await nextTick()

    const toasts = document.body.querySelectorAll('.p-toast-message')
    const closeButtons = document.body.querySelectorAll<HTMLButtonElement>('button[aria-label="关闭通知"]')
    expect(toasts).toHaveLength(3)
    expect(closeButtons).toHaveLength(3)
    expect(document.body.textContent).not.toContain('会被容量淘汰。')
    expect(document.body.textContent).toContain('节点已更新。')

    closeButtons[0]?.click()
    await nextTick()

    expect(feedbackState.toasts).toHaveLength(2)
    wrapper.unmount()
  })
})
