import { reactive, ref } from 'vue'
import type { AdminTask } from '../api/client'

const storageKey = 'zboard.admin.tracked-tasks'
const maxTrackedTasks = 20

function readTrackedIDs(): number[] {
  if (typeof window === 'undefined') return []
  try {
    const parsed = JSON.parse(window.sessionStorage.getItem(storageKey) || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.map(Number).filter(value => Number.isInteger(value) && value > 0).slice(0, maxTrackedTasks)
  } catch {
    return []
  }
}

export const trackedTaskIDs = ref<number[]>(readTrackedIDs())
export const trackedTaskSummaries = reactive<Record<number, AdminTask>>({})

function persist() {
  if (typeof window !== 'undefined') window.sessionStorage.setItem(storageKey, JSON.stringify(trackedTaskIDs.value))
}

export function trackAdminTask(task: AdminTask) {
  trackedTaskSummaries[task.id] = task
  trackedTaskIDs.value = [task.id, ...trackedTaskIDs.value.filter(id => id !== task.id)].slice(0, maxTrackedTasks)
  persist()
}

export function updateTrackedTask(task: AdminTask) {
  if (!trackedTaskIDs.value.includes(task.id)) return
  trackedTaskSummaries[task.id] = task
}

export function dismissTrackedTask(id: number) {
  trackedTaskIDs.value = trackedTaskIDs.value.filter(value => value !== id)
  delete trackedTaskSummaries[id]
  persist()
}

export function clearTrackedTasks() {
  trackedTaskIDs.value = []
  for (const key of Object.keys(trackedTaskSummaries)) delete trackedTaskSummaries[Number(key)]
  persist()
}
