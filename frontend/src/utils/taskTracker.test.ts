import { beforeEach, describe, expect, it } from 'vitest'
import { clearTrackedTasks, dismissTrackedTask, trackAdminTask, trackedTaskIDs, trackedTaskSummaries } from './taskTracker'

const task = (id: number) => ({ id, type: 'node_detect', scope: '{}', content: '{}', status: 1, errors: '', total: 10, current: 2, idempotency_key: String(id), priority: 0, attempts: 1, max_attempts: 3, created_at: '', updated_at: '' } as const)

describe('taskTracker', () => {
  beforeEach(() => clearTrackedTasks())

  it('deduplicates tracked tasks, keeps the newest first and supports dismissal', () => {
    trackAdminTask(task(1))
    trackAdminTask(task(2))
    trackAdminTask({ ...task(1), current: 5 })
    expect(trackedTaskIDs.value).toEqual([1, 2])
    expect(trackedTaskSummaries[1].current).toBe(5)
    dismissTrackedTask(1)
    expect(trackedTaskIDs.value).toEqual([2])
    expect(trackedTaskSummaries[1]).toBeUndefined()
  })
})
