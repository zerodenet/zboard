<template>
  <section>
    <PageHeader title="运营任务" description="批量执行配额调整或邮件通知，保留进度、重试次数和错误记录。" eyebrow="Operations">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />刷新</button><button class="button" type="button" @click="createOpen = true"><UiIcon name="plus" />创建任务</button></template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="task-summary">
      <div v-for="item in summary" :key="item.label"><span class="summary-dot" :data-tone="item.tone"></span><div><strong>{{ item.value }}</strong><span>{{ item.label }}</span></div></div>
    </div>

    <article class="panel">
      <header class="panel-header"><div><h2>任务队列</h2><p>失败任务可以重试；已完成的配额项不会重复执行。</p></div></header>
      <div class="panel-body toolbar"><div class="toolbar-group"><select v-model="typeFilter" aria-label="任务类型" @change="load"><option value="">全部类型</option><option value="quota">配额调整</option><option value="email">邮件通知</option></select><select v-model="statusFilter" aria-label="任务状态" @change="load"><option value="">全部状态</option><option value="0">等待执行</option><option value="1">执行中</option><option value="2">已完成</option><option value="3">执行失败</option></select></div><span class="muted">{{ tasks.length }} 个任务</span></div>
      <div v-if="tasks.length" class="task-list">
        <article v-for="task in tasks" :key="task.id" class="task-row">
          <div class="task-kind"><span><UiIcon :name="task.type === 'email' ? 'audit' : 'activity'" /></span><div><strong>{{ task.type === 'email' ? '邮件通知' : '配额调整' }}</strong><small>#{{ task.id }} · {{ scopeLabel(task.scope) }}</small></div></div>
          <div class="task-progress"><div><span>执行进度</span><strong>{{ task.current }}/{{ task.total }}</strong></div><div class="usage-track"><i :style="{ width: `${progress(task)}%` }"></i></div><small>第 {{ task.attempts }}/{{ task.max_attempts }} 次尝试</small></div>
          <div class="task-time"><span>创建时间</span><strong>{{ formatDateTime(task.created_at) }}</strong></div>
          <StatusBadge :tone="statusTone(task.status)">{{ statusName(task.status) }}</StatusBadge>
          <div class="task-action"><button v-if="task.status === 0 || task.status === 3" class="button button-secondary button-sm" :disabled="runningID === task.id || task.attempts >= task.max_attempts" type="button" @click="run(task.id)"><UiIcon name="play" />{{ runningID === task.id ? '启动中…' : task.status === 3 ? '重试' : '执行' }}</button></div>
          <details v-if="task.errors" class="task-error"><summary>查看错误详情</summary><pre>{{ task.errors }}</pre></details>
        </article>
      </div>
      <EmptyState v-else icon="tasks" title="暂无运营任务" description="创建配额调整或邮件任务后，执行进度会显示在这里。"><template #actions><button class="button" type="button" @click="createOpen = true"><UiIcon name="plus" />创建任务</button></template></EmptyState>
    </article>

    <ModalDialog :open="createOpen" title="创建运营任务" description="选择作用范围和执行内容；任务创建后可以立即运行或留在队列。" size="lg" :busy="creating" @close="createOpen = false">
      <form id="create-task-form" class="stack" @submit.prevent="createTask">
        <div class="form-grid">
          <label class="field"><span>任务类型</span><select v-model="form.type" @change="resetScope"><option value="quota">配额调整</option><option value="email">邮件通知</option></select></label>
          <label class="field"><span>作用范围</span><select v-model="scopeMode"><option value="all">全部有效目标</option><option v-if="form.type === 'quota'" value="subscriptions">指定订阅 ID</option><option value="users">指定用户 ID</option></select></label>
          <label v-if="scopeMode !== 'all'" class="field field-full"><span>{{ scopeMode === 'users' ? '用户 ID' : '订阅 ID' }}</span><input v-model.trim="scopeIDs" placeholder="例如：1, 2, 3" required /><small class="field-hint">使用英文逗号分隔，重复 ID 会自动去除。</small></label>
        </div>
        <div v-if="form.type === 'quota'" class="form-section"><div><strong>配额调整内容</strong><p>正数增加配额，负数扣减配额。</p></div><div class="form-grid"><label class="field"><span>调整量（MB）</span><input v-model.number="quota.delta_mb" type="number" required /></label><label class="field"><span>调整原因</span><input v-model.trim="quota.reason" minlength="3" maxlength="255" required placeholder="运营补偿" /></label></div></div>
        <div v-else class="form-section"><div><strong>邮件内容</strong><p>当前任务发送纯文本邮件。</p></div><div class="stack"><label class="field"><span>主题</span><input v-model.trim="email.subject" maxlength="200" required /></label><label class="field"><span>正文</span><textarea v-model="email.body" maxlength="100000" rows="7" required></textarea></label></div></div>
        <div class="form-grid"><label class="field"><span>最大尝试次数</span><input v-model.number="form.max_attempts" type="number" min="1" max="10" /></label><label class="check-field auto-run"><input v-model="form.auto_run" type="checkbox" /><span><strong>创建后立即执行</strong><br /><small class="field-hint">关闭后任务保持等待状态。</small></span></label></div>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="creating" @click="createOpen = false">取消</button><button class="button" form="create-task-form" type="submit" :disabled="creating">{{ creating ? '创建中…' : '创建任务' }}</button></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { createAdminTask, fetchAdminTasks, runAdminTask, type AdminTask } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { formatDateTime } from '../utils/format'

const tasks = ref<AdminTask[]>([])
const loading = ref(false)
const creating = ref(false)
const runningID = ref(0)
const error = ref('')
const message = ref('')
const typeFilter = ref('')
const route = useRoute()
const statusFilter = ref(String(route.query.status || ''))
const createOpen = ref(false)
const scopeMode = ref('all')
const scopeIDs = ref('')
const form = reactive({ type: 'quota' as 'quota' | 'email', max_attempts: 3, auto_run: false })
const quota = reactive({ delta_mb: 1024, reason: '' })
const email = reactive({ subject: '', body: '' })
const summary = computed(() => [
  { label: '等待执行', value: tasks.value.filter(task => task.status === 0).length, tone: 'neutral' },
  { label: '执行中', value: tasks.value.filter(task => task.status === 1).length, tone: 'info' },
  { label: '已完成', value: tasks.value.filter(task => task.status === 2).length, tone: 'success' },
  { label: '执行失败', value: tasks.value.filter(task => task.status === 3).length, tone: 'danger' }
])

function parseIDs() { return Array.from(new Set(scopeIDs.value.split(',').map(value => Number(value.trim())).filter(value => Number.isInteger(value) && value > 0))) }
function resetScope() { scopeMode.value = 'all'; scopeIDs.value = '' }
function progress(task: AdminTask) { return task.total ? Math.min(100, Math.round(task.current / task.total * 100)) : task.status === 2 ? 100 : 0 }
function statusName(status: number) { return ['等待执行', '执行中', '已完成', '执行失败'][status] || `未知状态 ${status}` }
function statusTone(status: number): 'neutral' | 'info' | 'success' | 'danger' { return ['neutral', 'info', 'success', 'danger'][status] as any || 'neutral' }
function scopeLabel(scope: string) { try { const data = JSON.parse(scope); if (data.all_active) return '全部有效目标'; if (data.user_ids) return `${data.user_ids.length} 个用户`; if (data.subscription_ids) return `${data.subscription_ids.length} 个订阅` } catch (_) { return scope || '未指定范围' }; return '未指定范围' }

async function load() {
  loading.value = true; error.value = ''
  try { tasks.value = await fetchAdminTasks({ type: typeFilter.value || undefined, status: statusFilter.value === '' ? undefined : Number(statusFilter.value), limit: 100 }) }
  catch (e: any) { error.value = e?.response?.data?.message || '任务列表加载失败。' }
  finally { loading.value = false }
}
async function createTask() {
  creating.value = true; error.value = ''; message.value = ''
  try {
    const ids = parseIDs(); if (scopeMode.value !== 'all' && ids.length === 0) throw new Error('请输入至少一个有效 ID。')
    const scope: any = scopeMode.value === 'all' ? { all_active: true } : {}
    if (scopeMode.value === 'users') scope.user_ids = ids
    if (scopeMode.value === 'subscriptions') scope.subscription_ids = ids
    await createAdminTask({ type: form.type, scope, content: form.type === 'quota' ? { ...quota } : { ...email }, max_attempts: form.max_attempts, auto_run: form.auto_run })
    createOpen.value = false; message.value = '任务已创建。'; quota.reason = ''; email.subject = ''; email.body = ''; await load()
  } catch (e: any) { error.value = e?.response?.data?.message || e?.message || '任务创建失败。' }
  finally { creating.value = false }
}
async function run(id: number) { runningID.value = id; error.value = ''; message.value = ''; try { await runAdminTask(id); message.value = `任务 #${id} 已启动。`; await load(); window.setTimeout(load, 1200) } catch (e: any) { error.value = e?.response?.data?.message || '任务启动失败。' } finally { runningID.value = 0 } }
onMounted(load)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.task-summary { display: grid; grid-template-columns: repeat(4, 1fr); margin-bottom: 16px; overflow: hidden; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--surface); }.task-summary > div { display: flex; align-items: center; gap: 11px; padding: 15px 18px; }.task-summary > div + div { border-left: 1px solid var(--line); }.summary-dot { width: 9px; height: 9px; border-radius: 50%; background: #98a2b3; box-shadow: 0 0 0 4px #f2f4f7; }.summary-dot[data-tone='info'] { background: #2e90fa; box-shadow: 0 0 0 4px var(--info-soft); }.summary-dot[data-tone='success'] { background: #12b76a; box-shadow: 0 0 0 4px var(--success-soft); }.summary-dot[data-tone='danger'] { background: #f04438; box-shadow: 0 0 0 4px var(--danger-soft); }.task-summary > div > div { display: grid; }.task-summary strong { font-size: 19px; }.task-summary span:not(.summary-dot) { color: var(--muted); font-size: 10px; }.toolbar select { width: 150px; min-height: 36px; }.task-list { display: grid; }.task-row { display: grid; grid-template-columns: minmax(190px, 1.3fr) minmax(190px, 1fr) minmax(150px, .8fr) auto auto; align-items: center; gap: 18px; padding: 16px 20px; border-top: 1px solid var(--line); }.task-kind { display: flex; align-items: center; gap: 10px; }.task-kind > span { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 9px; color: var(--primary); background: var(--primary-soft); }.task-kind div, .task-time { display: grid; gap: 3px; }.task-kind strong { font-size: 13px; }.task-kind small, .task-time span, .task-progress small { color: var(--muted); font-size: 10px; }.task-progress > div:first-child { display: flex; justify-content: space-between; margin-bottom: 7px; color: var(--muted); font-size: 10px; }.task-progress .usage-track { height: 5px; }.task-progress small { display: block; margin-top: 5px; }.task-time strong { font-size: 11px; font-weight: 600; }.task-error { grid-column: 1 / -1; padding: 10px 12px; border-radius: 8px; color: var(--danger); background: var(--danger-soft); font-size: 11px; }.task-error summary { cursor: pointer; font-weight: 650; }.task-error pre { white-space: pre-wrap; }.form-section { display: grid; gap: 14px; padding: 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.form-section > div:first-child strong { font-size: 13px; }.form-section > div:first-child p { margin: 3px 0 0; color: var(--muted); font-size: 11px; }.auto-run { align-self: end; min-height: 40px; }
@media (max-width: 1050px) { .task-row { grid-template-columns: minmax(190px, 1fr) minmax(180px, 1fr) auto; }.task-time { display: none; }.task-action { justify-self: end; } }
@media (max-width: 720px) { .task-summary { grid-template-columns: repeat(2, 1fr); }.task-summary > div:nth-child(3) { border-left: 0; border-top: 1px solid var(--line); }.task-summary > div:nth-child(4) { border-top: 1px solid var(--line); }.task-row { grid-template-columns: 1fr auto; }.task-progress { grid-column: 1 / -1; }.task-action { justify-self: end; } }
</style>
