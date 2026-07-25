import { reactive } from 'vue'

export type FeedbackTone = 'success' | 'danger' | 'info'

interface ConfirmOptions {
  title: string
  message: string
  description?: string
  confirmText?: string
  tone?: 'primary' | 'danger'
}

export interface ToastItem {
  id: number
  title: string
  message?: string
  tone: FeedbackTone
}

type PendingConfirm = ConfirmOptions & { resolve: (accepted: boolean) => void }

export const feedbackState = reactive({
  confirm: null as PendingConfirm | null,
  toasts: [] as ToastItem[]
})

const confirmQueue: PendingConfirm[] = []

let toastID = 0
export const MAX_ACTIVE_TOASTS = 3

export function confirmAction(options: ConfirmOptions) {
  return new Promise<boolean>((resolve) => {
    const pending = { ...options, resolve }
    if (feedbackState.confirm) {
      confirmQueue.push(pending)
      return
    }
    feedbackState.confirm = pending
  })
}

export function settleConfirm(accepted: boolean) {
  const pending = feedbackState.confirm
  if (!pending) return
  feedbackState.confirm = confirmQueue.shift() || null
  pending.resolve(accepted)
}

export function notify(title: string, message = '', tone: FeedbackTone = 'info') {
  const duplicate = feedbackState.toasts.find(item =>
    item.title === title && item.message === message && item.tone === tone,
  )
  if (duplicate) return duplicate.id

  const id = ++toastID
  while (feedbackState.toasts.length >= MAX_ACTIVE_TOASTS) feedbackState.toasts.shift()
  feedbackState.toasts.push({ id, title, message, tone })
  return id
}

export function dismissToast(id: number) {
  const index = feedbackState.toasts.findIndex(item => item.id === id)
  if (index >= 0) feedbackState.toasts.splice(index, 1)
}
