import { afterEach, describe, expect, it } from 'vitest'
import {
  MAX_ACTIVE_TOASTS,
  confirmAction,
  dismissToast,
  feedbackState,
  notify,
  settleConfirm,
} from './feedback'

afterEach(() => {
  while (feedbackState.confirm) settleConfirm(false)
  for (const item of [...feedbackState.toasts]) dismissToast(item.id)
})

describe('confirmation queue', () => {
  it('keeps concurrent confirmations in order without silently cancelling either request', async () => {
    const first = confirmAction({ title: '第一个', message: 'one' })
    const second = confirmAction({ title: '第二个', message: 'two' })

    expect(feedbackState.confirm?.title).toBe('第一个')
    settleConfirm(true)
    await expect(first).resolves.toBe(true)
    expect(feedbackState.confirm?.title).toBe('第二个')
    settleConfirm(false)
    await expect(second).resolves.toBe(false)
    expect(feedbackState.confirm).toBeNull()
  })
})

describe('toast queue', () => {
  it('deduplicates active messages and enforces a real three-toast cap', () => {
    const first = notify('保存完成', '节点已更新。', 'success')
    const duplicate = notify('保存完成', '节点已更新。', 'success')

    expect(duplicate).toBe(first)
    expect(feedbackState.toasts).toHaveLength(1)

    notify('第二条', '', 'info')
    notify('第三条', '', 'success')
    notify('第四条', '', 'danger')

    expect(feedbackState.toasts).toHaveLength(MAX_ACTIVE_TOASTS)
    expect(feedbackState.toasts.map(item => item.title)).toEqual(['第二条', '第三条', '第四条'])
  })
})
