<template>
  <aside v-if="taskIDs.length" class="task-tray" aria-label="后台任务" aria-live="polite">
    <header><div><UiIcon name="tasks" /><strong>后台任务</strong><span>{{ activeCount ? `${activeCount} 个执行中` : '已产生最终结果' }}</span></div><RouterLink :to="taskLink()">全部任务</RouterLink></header>
    <div class="task-tray-list">
      <article v-for="id in taskIDs" :key="id">
        <template v-if="tasks[id]">
          <div class="task-tray-heading"><div><strong>{{ taskTypeLabel(tasks[id].type) }}</strong><span>#{{ id }}</span></div><StatusBadge :tone="taskTone(tasks[id])" :icon="taskIcon(tasks[id])">{{ taskStatusLabel(tasks[id]) }}</StatusBadge></div>
          <div class="task-progress" role="progressbar" :aria-valuemin="0" :aria-valuemax="tasks[id].total" :aria-valuenow="tasks[id].current"><span :style="{ width: progress(tasks[id]) + '%' }"></span></div>
          <div class="task-tray-meta"><span>{{ tasks[id].current }} / {{ tasks[id].total }}</span><TimeBadge :value="tasks[id].finished_at || tasks[id].started_at || tasks[id].created_at" /></div>
          <p v-if="tasks[id].errors">{{ firstError(tasks[id].errors) }}</p>
          <div class="task-tray-actions"><RouterLink :to="taskLink(id)">查看详情</RouterLink><UiButton v-if="tasks[id].status >= 2" variant="ghost" size="sm" type="button" @click="dismissTrackedTask(id)">关闭</UiButton></div>
        </template>
        <div v-else class="task-loading"><span></span><span>正在读取任务 #{{ id }}…</span><UiButton variant="ghost" size="sm" type="button" aria-label="移除无法读取的任务" @click="dismissTrackedTask(id)">关闭</UiButton></div>
      </article>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchAdminTask, type AdminTask } from '../api/client'
import { dismissTrackedTask, trackedTaskIDs, trackedTaskSummaries, updateTrackedTask } from '../utils/taskTracker'
import { formatUnknownValue } from '../utils/format'
import { withAdminReturnTo } from '../utils/navigation'
import { normalizeOutput } from '../utils/output'
import StatusBadge from './StatusBadge.vue'
import UiIcon from './UiIcon.vue'

const route = useRoute()
const taskIDs = computed(() => trackedTaskIDs.value)
const tasks = trackedTaskSummaries
const activeCount = computed(() => taskIDs.value.filter(id => (tasks[id]?.status ?? 1) < 2).length)
let timer: number | undefined
let loading = false

function taskTypeLabel(type: AdminTask['type']) { return ({ quota: '配额调整', email: '邮件通知', node_detect: '节点状态检测', node_reconcile: '节点内核对齐', node_lifecycle: '节点生命周期', protocol_deploy: '协议批量发布', protocol_active: '协议批量启停', node_group_reconcile: '节点组交付对齐' } as Record<string, string>)[type] || formatUnknownValue('类型', type) }
function taskStatusLabel(task: AdminTask) { if (task.status === 0) return '等待执行'; if (task.status === 1) return '执行中'; if (task.status === 2) return '已完成'; if (task.status === 3) return Number(task.succeeded_count) > 0 && Number(task.failed_count) > 0 ? '部分失败' : '执行失败'; return formatUnknownValue('状态', task.status) }
function taskTone(task: AdminTask): 'neutral' | 'info' | 'success' | 'danger' | 'warning' { return task.status === 0 ? 'neutral' : task.status === 1 ? 'info' : task.status === 2 ? 'success' : Number(task.succeeded_count) > 0 && Number(task.failed_count) > 0 ? 'warning' : 'danger' }
function taskIcon(task: AdminTask) { return task.status === 0 ? 'minus' : task.status === 1 ? 'refresh' : task.status === 2 ? 'check' : 'alert' }
function progress(task: AdminTask) { return task.total ? Math.min(100, Math.round(task.current / task.total * 100)) : 0 }
function firstError(value: string) {
  const first = normalizeOutput(value).split('\n', 1)[0] || ''
  const characters = Array.from(first)
  return characters.length > 140 ? `${characters.slice(0, 140).join('')}…` : first
}
function taskLink(id?: number) {
  const query = id ? { task: String(id) } : {}
  return route.path === '/admin/tasks' ? { path: '/admin/tasks', query } : withAdminReturnTo('/admin/tasks', route.fullPath, query)
}

async function refresh() {
  if (loading || !taskIDs.value.length) return
  const unresolvedIDs = taskIDs.value.filter(id => !tasks[id] || tasks[id].status < 2)
  if (!unresolvedIDs.length) { stopTimer(); return }
  loading = true
  try {
    const results = await Promise.allSettled(unresolvedIDs.map(id => fetchAdminTask(id)))
    results.forEach(result => { if (result.status === 'fulfilled') updateTrackedTask(result.value) })
  } finally { loading = false; schedule() }
}

function stopTimer() {
  if (timer !== undefined) { window.clearTimeout(timer); timer = undefined }
}

function schedule() {
  stopTimer()
  if (taskIDs.value.some(id => !tasks[id] || tasks[id].status < 2)) {
    timer = window.setTimeout(() => { timer = undefined; void refresh() }, 2000)
  }
}

watch(taskIDs, () => { stopTimer(); void refresh() }, { deep: true })
onMounted(() => { void refresh() })
onBeforeUnmount(stopTimer)
</script>

<style scoped>
.task-tray{position:fixed;right:18px;bottom:18px;z-index:75;width:min(390px,calc(100vw - 28px));overflow:hidden;border:1px solid var(--line-strong);border-radius:13px;background:var(--surface);box-shadow:0 18px 45px var(--floating-shadow)}.task-tray>header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:12px 14px;border-bottom:1px solid var(--line);background:var(--surface-soft)}.task-tray>header>div{display:flex;align-items:center;gap:7px}.task-tray>header strong{font-size:11px}.task-tray>header span,.task-tray>header a{color:var(--muted);font-size:9px}.task-tray-list{max-height:420px;overflow:auto}.task-tray-list article{display:grid;gap:8px;padding:12px 14px}.task-tray-list article+article{border-top:1px solid var(--line)}.task-tray-heading,.task-tray-meta,.task-tray-actions,.task-loading{display:flex;align-items:center;justify-content:space-between;gap:10px}.task-tray-heading>div{display:flex;align-items:baseline;gap:6px}.task-tray-heading strong{font-size:11px}.task-tray-heading span,.task-tray-meta{color:var(--muted);font-size:9px}.task-progress{height:5px;overflow:hidden;border-radius:999px;background:var(--surface-soft)}.task-progress span{display:block;height:100%;border-radius:inherit;background:var(--primary);transition:width .18s ease}.task-tray p{margin:0;color:var(--danger);font-size:9px;line-height:1.45}.task-tray-actions{justify-content:flex-end}.task-tray-actions a{color:var(--primary);font-size:9px;font-weight:700;text-decoration:none}.task-loading{color:var(--muted);font-size:9px}.task-loading>span:first-child{width:7px;height:7px;border-radius:50%;background:var(--warning)}@media(max-width:680px){.task-tray{right:14px;bottom:14px}}
</style>
