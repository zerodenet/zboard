import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { dismissToast, feedbackState, notify } from '../utils/feedback'
import FeedbackHost from './FeedbackHost.vue'

afterEach(() => {
  for (const item of [...feedbackState.toasts]) dismissToast(item.id)
  document.body.innerHTML = ''
})

describe('FeedbackHost', () => {
  it('renders the deduplicated three-toast cap and synchronizes manual dismissal', async () => {
    const wrapper = mount(FeedbackHost, {
      attachTo: document.body,
    })

    notify('第一条', '会被容量淘汰。', 'success')
    notify('第一条', '会被容量淘汰。', 'success')
    notify('第二条', '节点已更新。', 'success')
    notify('第三条', '订单已更新。', 'success')
    notify('第四条', '设置已保存。', 'success')
    await nextTick()
    await nextTick()

    const toasts = document.body.querySelectorAll('.ui-toast')
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
