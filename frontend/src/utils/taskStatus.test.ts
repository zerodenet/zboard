import { describe, expect, it } from 'vitest'
import type { AdminTask, AdminTaskItem } from '../api/client'
import { canReviewMailItem, mailSubmissionStatus, taskHasLiveLease, taskItemStatus, taskStatus } from './taskStatus'

const task = { type: 'email', status: 2, total: 1 } as AdminTask
const item = { status: 2, delivery_state: 'accepted' } as AdminTaskItem
describe('business results across task surfaces', () => {
  it.each([
    [0, '', '待发送', 'neutral'], [1, 'unknown', '发送中', 'info'],
    [2, 'accepted', '已提交', 'success'], [3, 'not_accepted', '提交失败', 'danger'],
    [3, 'unknown', '待核验', 'warning'], [2, 'suppressed', '已取消', 'neutral'],
    [2, '', '结果未记录', 'neutral'], [0, 'not_accepted', '待发送', 'neutral'],
    [3, '', '发送失败', 'danger'], [99, '', '状态未知', 'warning'],
  ])('projects item %s/%s as %s', (status, delivery_state, label, tone) => {
    expect(taskItemStatus({ ...task, status: status === 1 ? 1 : 2 }, { ...item, status: Number(status), delivery_state } as AdminTaskItem)).toMatchObject({ label, tone })
  })
  it('keeps generic task execution separate from mail submission', () => {
    expect(taskItemStatus({ ...task, type: 'quota' }, item).label).toBe('已完成')
    expect(taskStatus({ ...task, needs_review_count: 1, status: 3 }).label).toBe('待核验')
    expect(taskStatus({ ...task, suppressed_count: 1 })).toMatchObject({ label: '已取消', tone: 'neutral' })
    expect(taskStatus(task)).toMatchObject({ label: '已处理', tone: 'neutral' })
    expect(mailSubmissionStatus('unknown').tone).toBe('warning')
  })
  it('matches server review eligibility including expired running leases', () => {
    const unknown = { ...item, status: 1, delivery_state: 'unknown' } as AdminTaskItem
    expect(canReviewMailItem({ ...task, status: 1 }, unknown)).toBe(false)
    expect(taskHasLiveLease({ status: 1, locked_until: 'invalid' })).toBe(true)
    const expired = { ...task, status: 1, locked_until: '2000-01-01T00:00:00Z', needs_review_count: 1 }
    expect(canReviewMailItem(expired, unknown)).toBe(true)
    expect(taskItemStatus(expired, unknown).label).toBe('待核验')
    expect(taskStatus(expired).label).toBe('待核验')
    expect(canReviewMailItem({ ...task, type: 'quota' }, unknown)).toBe(false)
  })
})
