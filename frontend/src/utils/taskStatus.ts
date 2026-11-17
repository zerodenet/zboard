import type { AdminTask, AdminTaskItem } from '../api/client'

type Tone = 'neutral' | 'info' | 'success' | 'warning' | 'danger'
export interface TaskStatus { label: string; tone: Tone; icon: string }
const state = (label: string, tone: Tone, icon: string): TaskStatus => ({ label, tone, icon })

// Submission acknowledges the mail server, never delivery to an inbox.
export function mailSubmissionStatus(acceptance?: string): TaskStatus {
  if (acceptance === 'accepted') return state('已提交', 'success', 'check')
  if (acceptance === 'not_accepted') return state('提交失败', 'danger', 'close')
  if (acceptance === 'unknown') return state('待核验', 'warning', 'alert')
  if (acceptance === 'suppressed') return state('已取消', 'neutral', 'minus')
  return state('结果未记录', 'neutral', 'minus')
}

export function taskHasLiveLease(task: Pick<AdminTask, 'status' | 'locked_until'>, now = Date.now()): boolean {
  // Match the server: a running task without a lease timestamp cannot be reviewed.
  return task.status === 1 && (!task.locked_until || !(Date.parse(task.locked_until) <= now))
}

export function canReviewMailItem(task: AdminTask, item: AdminTaskItem): boolean {
  return task.type === 'email' && item.delivery_state === 'unknown' && !taskHasLiveLease(task)
}

export function taskItemStatus(task: AdminTask, item: AdminTaskItem): TaskStatus {
  if (task.type !== 'email') return executionStatus(item.status)
  if (item.delivery_state === 'suppressed') return mailSubmissionStatus('suppressed')
  if (item.status === 1 && taskHasLiveLease(task)) return state('发送中', 'info', 'refresh')
  if (item.delivery_state === 'unknown' || item.delivery_state === 'accepted') return mailSubmissionStatus(item.delivery_state)
  if (item.status === 0) return state('待发送', 'neutral', 'minus')
  if (item.delivery_state === 'not_accepted') return mailSubmissionStatus(item.delivery_state)
  if (item.status === 1) return state('待核验', 'warning', 'alert')
  if (item.status === 3) return state('发送失败', 'danger', 'close')
  if (item.status === 2) return mailSubmissionStatus()
  return state('状态未知', 'warning', 'alert')
}

function executionStatus(status: number): TaskStatus {
  return [state('等待执行', 'neutral', 'minus'), state('执行中', 'info', 'refresh'), state('已完成', 'success', 'check'), state('执行失败', 'danger', 'close')][status] || state('状态未知', 'warning', 'alert')
}

export function taskStatus(task: AdminTask): TaskStatus {
  if (task.type === 'email') {
    if (taskHasLiveLease(task)) return executionStatus(1)
    if (Number(task.needs_review_count) > 0) return state('待核验', 'warning', 'alert')
    if (task.status === 2) return Number(task.suppressed_count) > 0 && Number(task.suppressed_count) === Number(task.total)
      ? state('已取消', 'neutral', 'minus') : state('已处理', 'neutral', 'minus')
  }
  if (task.status === 3 && Number(task.succeeded_count) > 0 && Number(task.failed_count) > 0) return state('部分失败', 'warning', 'alert')
  return executionStatus(task.status)
}
