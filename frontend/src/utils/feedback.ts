import { reactive } from 'vue'

export type FeedbackTone = 'success' | 'danger' | 'info'

interface ConfirmOptions {
  title: string
  message: string
  description?: string
  confirmText?: string
  tone?: 'primary' | 'danger'
}

interface ToastItem {
  id: number
  title: string
  message?: string
  tone: FeedbackTone
}

export const feedbackState = reactive({
  confirm: null as (ConfirmOptions & { resolve: (accepted: boolean) => void }) | null,
  toasts: [] as ToastItem[]
})

let toastID = 0

export function confirmAction(options: ConfirmOptions) {
  return new Promise<boolean>((resolve) => {
    feedbackState.confirm?.resolve(false)
    feedbackState.confirm = { ...options, resolve }
  })
}

export function settleConfirm(accepted: boolean) {
  const pending = feedbackState.confirm
  if (!pending) return
  feedbackState.confirm = null
  pending.resolve(accepted)
}

export function notify(title: string, message = '', tone: FeedbackTone = 'info') {
  const id = ++toastID
  feedbackState.toasts.push({ id, title, message, tone })
  window.setTimeout(() => dismissToast(id), tone === 'danger' ? 6500 : 4200)
}

export function dismissToast(id: number) {
  const index = feedbackState.toasts.findIndex(item => item.id === id)
  if (index >= 0) feedbackState.toasts.splice(index, 1)
}
