<template>
  <section class="standard-page">
    <PageHeader title="运营任务" description="追踪配额、通知、节点检测与协议发布等后台任务，保留每个目标的最终结果。" eyebrow="Operations">
      <template #actions><PageRefreshButton label="刷新运营任务" :loading="loading || summaryLoading" @click="refreshAll" /><UiButton type="button" @click="openCreate"><UiIcon name="plus" />创建任务</UiButton></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="任务操作已完成" error-title="任务操作失败" />

    <PageAlert v-if="summaryError" tone="danger" title="任务概览加载失败">{{ summaryError }}</PageAlert>
    <section class="task-overview" aria-label="持久化任务状态概览">
      <button v-for="card in summaryCards" :key="card.key" type="button" class="task-overview-card" :class="{ active: statusFilter === card.status }" @click="filterSummary(card.status)"><span>{{ card.label }}</span><strong>{{ formatNumber(card.value) }}</strong><small>{{ card.description }}</small></button>
      <div class="task-overview-progress"><div><span>活动任务目标进度</span><strong>{{ formatNumber(summary?.active_current || 0) }} / {{ formatNumber(summary?.active_total || 0) }}</strong></div><div class="progress-track" role="progressbar" :aria-valuenow="activeProgress" aria-valuemin="0" aria-valuemax="100"><i :style="{ width: `${activeProgress}%` }" /></div><small>等待目标 {{ formatNumber(summary?.pending_targets || 0) }} · 执行中 {{ formatNumber(summary?.running_targets || 0) }} · 失败 {{ formatNumber(summary?.failed_targets || 0) }}</small></div>
    </section>

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters><WorkbenchFilterBar :active="Boolean(typeFilter || statusFilter)" @clear="clearFilters"><WorkbenchFilterSelect v-model="typeFilter" label="任务类型" :options="taskTypeOptions" @apply="applyFilters" /><WorkbenchFilterSelect v-model="statusFilter" label="任务状态" :options="taskStatusOptions" @apply="applyFilters" /></WorkbenchFilterBar></template>
      <DataTable v-if="tasks.length" caption="后台任务列表；数量直接显示数字，状态使用图标标签" :row-count="total" :min-width="1120" table-class="task-table"><thead><tr><th class="table-primary-column">任务</th><th data-column-priority="2">范围</th><th class="numeric-column">已处理</th><th class="numeric-column">总数</th><th class="numeric-column" data-column-priority="3">尝试次数</th><th data-column-priority="2">创建时间</th><th>状态</th><th class="numeric-column">失败数</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="task in tasks" :key="task.id"><td class="table-primary-column"><div class="cell-title"><strong>{{ taskTypeLabel(task.type) }}</strong><span>#{{ task.id }} · {{ taskOriginLabel(task) }}</span></div></td><td data-column-priority="2">{{ scopeLabel(task.scope) }}</td><td class="numeric-column"><div class="row-progress"><strong>{{ formatNumber(task.current) }}</strong><span><i :style="{ width: `${taskProgress(task)}%` }" /></span></div></td><td class="numeric-column">{{ formatNumber(task.total) }}</td><td class="numeric-column" data-column-priority="3">{{ task.attempts }} / {{ task.max_attempts }}</td><td data-column-priority="2"><TimeBadge :value="task.created_at" /></td><td><StatusBadge :tone="statusTone(task)" :icon="statusIcon(task)">{{ statusName(task) }}</StatusBadge></td><td class="numeric-column"><span v-if="task.failed_count || task.errors" class="error-count">{{ formatNumber(task.failed_count ?? errorCount(task.errors || '')) }}</span><span v-else>0</span></td><td class="table-action-column"><RowActions :label="`任务 #${task.id} 的操作`" :trigger-key="`task-${task.id}`"><UiButton variant="ghost" size="sm" type="button" @click="openTask(task)">查看结果</UiButton><UiButton v-if="task.status === 0 || task.status === 3" variant="secondary" size="sm" :loading="runningID === task.id" :disabled="task.attempts >= task.max_attempts" type="button" @click="run(task.id)"><UiIcon name="play" />{{ task.status === 3 ? '重试失败项' : '执行' }}</UiButton></RowActions></td></tr></tbody></DataTable>
      <EmptyState v-else icon="tasks" title="暂无运营任务" description="创建配额调整或邮件任务后，执行进度会显示在这里。"><template #actions><UiButton type="button" @click="openCreate"><UiIcon name="plus" />创建任务</UiButton></template></EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(selectedTask)" :title="selectedTask ? `${taskTypeLabel(selectedTask.type)} #${selectedTask.id}` : '任务结果'" description="任务状态来自后端持久化结果；失败项可以在任务级重试，已完成项不会重复执行。" @close="closeTask">
      <div v-if="selectedTask" class="stack task-detail">
        <div class="task-facts"><div><span>状态</span><StatusBadge :tone="statusTone(selectedTask)" :icon="statusIcon(selectedTask)">{{ statusName(selectedTask) }}</StatusBadge></div><div><span>处理进度</span><strong>{{ selectedTask.current }} / {{ selectedTask.total }}</strong></div><div><span>任务范围</span><strong>{{ scopeLabel(selectedTask.scope) }}</strong></div><div><span>尝试次数</span><strong>{{ selectedTask.attempts }} / {{ selectedTask.max_attempts }}</strong></div><div><span>开始时间</span><TimeBadge :value="selectedTask.started_at || selectedTask.created_at" /></div><div><span>结束时间</span><TimeBadge :value="selectedTask.finished_at" /></div></div>
        <PageAlert v-if="selectedTask.errors" :tone="Number(selectedTask.succeeded_count) > 0 ? 'warning' : 'danger'" :title="Number(selectedTask.succeeded_count) > 0 ? '任务存在部分失败' : '任务执行失败'"><OutputBlock :value="selectedTask.errors" label="任务错误汇总" tone="danger" /></PageAlert>
        <section class="task-items-section"><header><div><h3>目标结果</h3><p>每行对应一个持久化 TaskItem，大任务不会一次加载全部结果。</p></div><UiSelect v-model="itemStatusFilter" aria-label="目标状态" :options="taskStatusOptions" @change="applyItemFilter" /></header>
          <PageAlert v-if="itemError" tone="danger" title="目标结果加载失败">{{ itemError }}</PageAlert>
          <DataTable v-if="taskItems.length" caption="任务目标执行结果" :row-count="itemTotal" :min-width="760" table-class="task-item-table"><thead><tr><th class="table-primary-column">目标</th><th>状态</th><th class="numeric-column" data-column-priority="3">尝试次数</th><th data-column-priority="2">完成时间</th><th>错误</th></tr></thead><tbody><tr v-for="item in taskItems" :key="item.id"><td class="table-primary-column"><div class="cell-title"><strong>{{ targetTypeLabel(item.target_type) }} #{{ item.target_id }}</strong><span>TaskItem #{{ item.id }}</span></div></td><td><StatusBadge :tone="itemStatusTone(item.status)" :icon="itemStatusIcon(item.status)">{{ itemStatusName(item.status) }}</StatusBadge></td><td class="numeric-column" data-column-priority="3">{{ item.attempts }}</td><td data-column-priority="2"><TimeBadge :value="item.finished_at || item.started_at" /></td><td><OutputBlock v-if="item.error" :value="item.error" :label="`目标 ${item.target_id} 错误`" tone="danger" :max-length="220" /><span v-else>—</span></td></tr></tbody></DataTable>
          <EmptyState v-else-if="!itemLoading" icon="tasks" title="没有匹配的目标结果" description="调整目标状态筛选，或等待任务开始执行。" />
          <TablePager :total="itemTotal" :offset="itemOffset" :limit="itemLimit" :loading="itemLoading" @change="changeItemPage" />
        </section>
      </div>
    </DetailDrawer>

    <ModalDialog :open="createOpen" :dirty="createState.dirty.value" title="创建运营任务" description="选择作用范围和执行内容；任务创建后可以立即运行或留在队列。" size="lg" :busy="creating" @close="createOpen = false">
      <form id="create-task-form" ref="createFormElement" class="stack" novalidate @submit.prevent="createTask">
        <PageAlert v-if="createErrors.formError.value" tone="danger" title="无法创建任务">{{ createErrors.formError.value }}</PageAlert>
        <div class="form-grid">
          <FormField v-slot="{ controlAttrs }" label="任务类型" name="task-type" :error="createErrors.fields.type"><UiSelect v-model="form.type" v-bind="controlAttrs" :options="createTaskTypeOptions" @change="resetScope" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="作用范围" name="task-scope" :error="createErrors.fields.scope_mode"><UiSelect v-model="scopeMode" v-bind="controlAttrs" :options="scopeOptions" /></FormField>
          <FormField v-if="scopeMode !== 'all'" v-slot="{ controlAttrs }" :label="scopeMode === 'users' ? '选择用户' : '选择订阅'" name="task-scope-targets" :error="createErrors.fields.scope_ids" hint="按业务名称搜索并多选，内部 ID 仅用于提交。" required full><TaskTargetLookup v-model="selectedScopeIDs" v-bind="controlAttrs" :mode="scopeMode === 'users' ? 'users' : 'subscriptions'" /></FormField>
        </div>
        <div v-if="form.type === 'quota'" class="form-section"><div><strong>配额调整内容</strong><p>正数增加配额，负数扣减配额。</p></div><div class="form-grid"><FormField v-slot="{ controlAttrs }" label="调整量" name="task-quota-delta" :error="createErrors.fields['quota.delta_mb']" hint="统一以 MB 输入；允许负数。" required><UiNumberInput v-model="quota.delta_mb" v-bind="controlAttrs" suffix=" MB" /></FormField><FormField v-slot="{ controlAttrs }" label="调整原因" name="task-quota-reason" :error="createErrors.fields['quota.reason']" required><UiInput v-model.trim="quota.reason" v-bind="controlAttrs" minlength="3" maxlength="255" placeholder="运营补偿" /></FormField></div></div>
        <div v-else class="form-section"><div><strong>邮件内容</strong><p>选择运营模板后会复制为本次任务草稿；后续修改模板不会改写任务历史。</p></div><div class="stack"><FormField v-slot="{ controlAttrs }" label="运营模板" name="task-email-template" hint="仅显示已启用模板；也可以保留“自定义内容”从空白开始。"><UiSelect v-model="selectedEmailTemplateID" v-bind="controlAttrs" :options="emailTemplateOptions" @change="applyEmailTemplate" /></FormField><PageAlert tone="info" title="收件人变量">主题和正文支持 <span class="mono">{{emailTemplateVariables}}</span>，发送时按每位用户替换。</PageAlert><FormField v-slot="{ controlAttrs }" label="主题" name="task-email-subject" :error="createErrors.fields['email.subject']" required><UiInput v-model.trim="email.subject" v-bind="controlAttrs" maxlength="200" /></FormField><FormField v-slot="{ controlAttrs }" label="正文" name="task-email-body" :error="createErrors.fields['email.body']" required><UiTextarea v-model="email.body" v-bind="controlAttrs" maxlength="100000" rows="7" /></FormField></div></div>
        <div class="form-grid"><FormField v-slot="{ controlAttrs }" label="最大尝试次数" name="task-max-attempts" :error="createErrors.fields.max_attempts"><UiNumberInput v-model="form.max_attempts" v-bind="controlAttrs" :min="1" :max="10" inputmode="numeric" /></FormField><label class="check-field auto-run"><UiCheckbox v-model="form.auto_run" /><span><strong>创建后立即执行</strong><br /><small class="field-hint">关闭后任务保持等待状态。</small></span></label></div>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="creating" @click="requestClose">取消</UiButton><UiButton form="create-task-form" type="submit" :loading="creating">创建任务</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createAdminTask, fetchAdminTask, fetchAdminTaskItems, fetchAdminTasksPage, fetchAdminTaskSummary, fetchEmailTemplates, runAdminTask, type AdminTask, type AdminTaskItem, type AdminTaskSummary, type EmailTemplate } from '../api/client'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import OutputBlock from '../components/OutputBlock.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TaskTargetLookup from '../components/TaskTargetLookup.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { formatNumber, formatUnknownValue } from '../utils/format'
import { preserveAdminReturnTo } from '../utils/navigation'
import { normalizeOutput, truncateOutput } from '../utils/output'
import { trackAdminTask } from '../utils/taskTracker'
import { collectFieldErrors, isIntegerInRange, isOneOf, isUtf8LengthInRange } from '../utils/validation'

const selectedTask = ref<AdminTask | null>(null)
const creating = ref(false)
const runningID = ref(0)
const summary = ref<AdminTaskSummary | null>(null)
const summaryLoading = ref(false)
const summaryError = ref('')
const message = ref('')
const route = useRoute()
const router = useRouter()
const typeFilter = ref(String(route.query.type || ''))
const statusFilter = ref(String(route.query.status || ''))
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const itemOffset = ref(0), itemLimit = ref(50)
const itemStatusFilter = ref('')
const createOpen = ref(false)
const createFormElement = ref<HTMLElement | null>(null)
const scopeMode = ref<'all' | 'users' | 'subscriptions'>('all')
const selectedScopeIDs = ref<number[]>([])
const form = reactive({ type: 'quota' as 'quota' | 'email', max_attempts: 3, auto_run: false })
const quota = reactive({ delta_mb: 1024, reason: '' })
const email = reactive({ subject: '', body: '' })
const emailTemplates = ref<EmailTemplate[]>([])
const selectedEmailTemplateID = ref(0)
const emailTemplateVariables = '{{site_name}}、{{site_url}}、{{user_email}}、{{account_name}}、{{registered_at}}、{{current_date}}'
const createState = useDirtyForm(() => ({ form, quota, email, scope_mode: scopeMode.value, scope_ids: [...selectedScopeIDs.value] }))
useUnsavedChangesGuard(
  () => createOpen.value && createState.dirty.value,
  () => createState.confirmDiscard({
    title: '放弃任务草稿？',
    message: '离开运营任务后，尚未创建的作用范围和任务内容将丢失。',
    confirmText: '离开页面',
  }),
)
const createErrors = useFormErrors()
const writableTaskTypes = ['quota', 'email'] as const
const scopeModes = ['all', 'subscriptions', 'users'] as const
let detailPollTimer: number | undefined
let overviewPollTimer: number | undefined
const taskTypeOptions = [{ label: '全部类型', value: '' }, { label: '配额调整', value: 'quota' }, { label: '邮件通知', value: 'email' }, { label: '节点状态检测', value: 'node_detect' }, { label: '节点内核对齐', value: 'node_reconcile' }, { label: '节点生命周期', value: 'node_lifecycle' }, { label: '协议批量发布', value: 'protocol_deploy' }, { label: '协议批量启停', value: 'protocol_active' }, { label: '节点组交付对齐', value: 'node_group_reconcile' }, { label: 'VPS 系统自动化', value: 'node_system_action' }]
const taskStatusOptions = [{ label: '全部状态', value: '' }, { label: '等待执行', value: '0' }, { label: '执行中', value: '1' }, { label: '已完成', value: '2' }, { label: '执行失败', value: '3' }]
const summaryCards = computed(() => [
  { key: 'all', status: '', label: '全部任务', value: summary.value?.total || 0, description: '持久化记录' },
  { key: 'pending', status: '0', label: '等待执行', value: summary.value?.pending || 0, description: '尚未消费' },
  { key: 'running', status: '1', label: '正在执行', value: summary.value?.running || 0, description: '后台处理中' },
  { key: 'completed', status: '2', label: '执行完成', value: summary.value?.completed || 0, description: '全部成功' },
  { key: 'failed', status: '3', label: '执行失败', value: summary.value?.failed || 0, description: '可查看或重试' },
])
const activeProgress = computed(() => summary.value?.active_total ? Math.min(100, Math.round(summary.value.active_current * 100 / summary.value.active_total)) : 0)
const createTaskTypeOptions = taskTypeOptions.filter(option => option.value === 'quota' || option.value === 'email')
const emailTemplateOptions = computed(() => [
  { label: '自定义内容', value: 0 },
  ...emailTemplates.value.filter(item => item.is_active).map(item => ({ label: item.name, value: item.id })),
])
const scopeOptions = computed(() => [{ label: '全部有效目标', value: 'all' }, ...(form.type === 'quota' ? [{ label: '选择指定订阅', value: 'subscriptions' }] : []), { label: '选择指定用户', value: 'users' }])
const { items: tasks, total, loading, refreshing, error, load } = useRemoteTable<AdminTask>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchAdminTasksPage({ type: typeFilter.value || undefined, status: statusFilter.value === '' ? undefined : Number(statusFilter.value), offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '任务列表加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
const { items: taskItems, total: itemTotal, loading: itemLoading, error: itemError, load: loadTaskItems, invalidate: invalidateTaskItems } = useRemoteTable<AdminTaskItem>({
  offset: itemOffset,
  limit: itemLimit,
  fetchPage: ({ signal }) => selectedTask.value
    ? fetchAdminTaskItems(selectedTask.value.id, { status: itemStatusFilter.value === '' ? undefined : Number(itemStatusFilter.value), offset: itemOffset.value, limit: itemLimit.value }, { signal })
    : Promise.resolve({ items: [], total: 0 }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '目标结果加载失败。',
})
function parseIDs() {
  const values = selectedScopeIDs.value.map(Number)
  const valid = values.length > 0 && values.every(value => Number.isSafeInteger(value) && value > 0)
  return { valid, ids: valid ? Array.from(new Set(values)) : [] }
}
function resetScope() { scopeMode.value = 'all'; selectedScopeIDs.value = []; createErrors.clear() }
function createTaskFieldMap(): Record<string, string> {
  return {
    type: 'type',
    scope: scopeMode.value === 'all' ? 'scope_mode' : 'scope_ids',
    content: form.type === 'quota' ? 'quota.delta_mb' : 'email.subject',
    'content.delta_mb': 'quota.delta_mb',
    'content.reason': 'quota.reason',
    'content.subject': 'email.subject',
    'content.body': 'email.body',
    max_attempts: 'max_attempts',
  }
}
for (const [source, field] of [
  [() => form.type, 'type'], [() => scopeMode.value, 'scope_mode'], [() => selectedScopeIDs.value.join(','), 'scope_ids'],
  [() => form.max_attempts, 'max_attempts'], [() => quota.delta_mb, 'quota.delta_mb'],
  [() => quota.reason, 'quota.reason'], [() => email.subject, 'email.subject'], [() => email.body, 'email.body'],
] as Array<[() => unknown, string]>) watch(source, () => createErrors.clear(field))
function taskTypeLabel(type: AdminTask['type'] | 'node_system_action') { return ({ quota: '配额调整', email: '邮件通知', node_detect: '节点状态检测', node_reconcile: '节点内核对齐', node_lifecycle: '节点生命周期', protocol_deploy: '协议批量发布', protocol_active: '协议批量启停', node_group_reconcile: '节点组交付对齐', node_system_action: 'VPS 系统自动化' } as Record<string, string>)[type] || formatUnknownValue('类型', type) }
function statusName(task: AdminTask) { if (task.status === 0) return '等待执行'; if (task.status === 1) return '执行中'; if (task.status === 2) return '已完成'; if (task.status === 3) return Number(task.succeeded_count) > 0 && Number(task.failed_count) > 0 ? '部分失败' : '执行失败'; return formatUnknownValue('状态', task.status) }
function statusTone(task: AdminTask): 'neutral' | 'info' | 'success' | 'danger' | 'warning' { return task.status === 0 ? 'neutral' : task.status === 1 ? 'info' : task.status === 2 ? 'success' : Number(task.succeeded_count) > 0 && Number(task.failed_count) > 0 ? 'warning' : 'danger' }
function statusIcon(task: AdminTask) { return task.status === 0 ? 'minus' : task.status === 1 ? 'refresh' : task.status === 2 ? 'check' : 'alert' }
function scopeLabel(scope: string) { try { const data = JSON.parse(scope); if (data.all_active) return '全部有效目标'; if (data.all_matching) return '全部筛选结果'; if (data.node_group_id) return `节点组 #${data.node_group_id} · ${data.node_ids?.length || 0} 个节点`; if (data.user_ids) return `${data.user_ids.length} 个用户`; if (data.subscription_ids) return `${data.subscription_ids.length} 个订阅`; if (data.node_ids) return `${data.node_ids.length} 个节点`; if (data.protocol_endpoint_ids) return `${data.protocol_endpoint_ids.length} 个协议服务` } catch (_) { return `无法解析范围（${truncateOutput(normalizeOutput(scope), 80)}）` }; return '未指定范围' }
function errorCount(errors: string) { return errors.split(/\r?\n/).filter(Boolean).length }
function targetTypeLabel(type: string) { return ({ user: '用户', subscription: '订阅', node: '节点', protocol_endpoint: '协议服务', node_group: '节点组凭证' } as Record<string, string>)[type] || formatUnknownValue('目标类型', type) }
function itemStatusName(status: number) { return ['等待执行', '执行中', '已完成', '执行失败'][status] || formatUnknownValue('状态', status) }
function itemStatusTone(status: number): 'neutral' | 'info' | 'success' | 'danger' { return ['neutral', 'info', 'success', 'danger'][status] as any || 'neutral' }
function itemStatusIcon(status: number) { return ['minus', 'refresh', 'check', 'alert'][status] || 'minus' }
function taskProgress(task: AdminTask) { return task.total > 0 ? Math.min(100, Math.round(task.current * 100 / task.total)) : 0 }
function taskOriginLabel(task: AdminTask) { if (task.idempotency_key?.startsWith('registration-welcome:')) return '系统注册通知'; if (task.idempotency_key?.startsWith('operation:') || task.idempotency_key?.startsWith('node-')) return '系统任务'; return '运营创建' }
async function loadSummary() { summaryLoading.value = true; summaryError.value = ''; try { summary.value = await fetchAdminTaskSummary() } catch (cause: any) { summaryError.value = cause?.response?.data?.message || '持久化任务概览加载失败。' } finally { summaryLoading.value = false } }
async function refreshAll() { await Promise.all([load(), loadSummary()]) }
async function filterSummary(status: string) { statusFilter.value = status; await applyFilters() }

async function syncURL(replace = false) { const page = Math.floor(offset.value / limit.value) + 1; const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(typeFilter.value ? { type: typeFilter.value } : {}), ...(statusFilter.value ? { status: statusFilter.value } : {}), ...(page > 1 ? { page: String(page) } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}), ...(selectedTask.value ? { task: String(selectedTask.value.id) } : {}) } }; await (replace ? router.replace(location) : router.push(location)) }
async function applyFilters() { offset.value = 0; await syncURL(); await load() }
async function clearFilters() { typeFilter.value = ''; statusFilter.value = ''; await applyFilters() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await load() }
async function openCreate() {
  Object.assign(form, { type: 'quota', max_attempts: 3, auto_run: false })
  Object.assign(quota, { delta_mb: 1024, reason: '' })
  Object.assign(email, { subject: '', body: '' })
  selectedEmailTemplateID.value = 0
  scopeMode.value = 'all'; selectedScopeIDs.value = []; createErrors.clear()
  createState.markClean(); createOpen.value = true
  try { emailTemplates.value = await fetchEmailTemplates('operational') }
  catch (cause: any) { error.value = cause?.response?.data?.message || '运营邮件模板加载失败；仍可创建自定义邮件。' }
}
function applyEmailTemplate() {
  const template = emailTemplates.value.find(item => item.id === Number(selectedEmailTemplateID.value))
  if (!template) return
  email.subject = template.subject_template
  email.body = template.body_template
  createErrors.clear('email.subject'); createErrors.clear('email.body')
}
async function createTask() {
  const parsed = parseIDs()
  const quotaDeltaValid = isIntegerInRange(quota.delta_mb, -1_000_000_000, 1_000_000_000) && quota.delta_mb !== 0
  const valid = await createErrors.applyValidation(collectFieldErrors({
    type: !isOneOf(form.type, writableTaskTypes) && '请选择配额调整或邮件通知任务。',
    scope_mode: !isOneOf(scopeMode.value, scopeModes) && '请选择有效的作用范围。',
    scope_ids: scopeMode.value !== 'all' && !parsed.valid && '请至少选择一个有效目标。',
    max_attempts: !isIntegerInRange(form.max_attempts, 1, 10) && '最大尝试次数必须在 1 到 10 之间。',
    'quota.delta_mb': form.type === 'quota' && !quotaDeltaValid && '调整量必须为非零整数，且在 -1000000000 到 1000000000 MB 之间。',
    'quota.reason': form.type === 'quota' && !isUtf8LengthInRange(quota.reason, 3, 255, true) && '调整原因需包含 3 到 255 个 UTF-8 字节。',
    'email.subject': form.type === 'email' && (!isUtf8LengthInRange(email.subject, 1, 200, true) || /[\r\n]/.test(email.subject)) && '邮件主题需包含 1 到 200 个 UTF-8 字节，且不能换行。',
    'email.body': form.type === 'email' && !isUtf8LengthInRange(email.body, 1, 100000, true) && '邮件正文需包含 1 到 100000 个 UTF-8 字节。',
  }), createFormElement, '请更正标记字段后再创建任务。')
  if (!valid) return
  creating.value = true; message.value = ''
  try {
    const ids = parsed.ids
    const scope: any = scopeMode.value === 'all' ? { all_active: true } : {}
    if (scopeMode.value === 'users') scope.user_ids = ids
    if (scopeMode.value === 'subscriptions') scope.subscription_ids = ids
    const task = await createAdminTask({ type: form.type, scope, content: form.type === 'quota' ? { ...quota } : { ...email }, max_attempts: form.max_attempts, auto_run: form.auto_run })
    if (form.auto_run) trackAdminTask(task)
    createOpen.value = false; message.value = '任务已创建。'; quota.reason = ''; email.subject = ''; email.body = ''; await refreshAll()
  } catch (e: any) { await createErrors.applyApiError(e, '任务创建失败，请检查表单内容。', createFormElement, createTaskFieldMap()) }
  finally { creating.value = false }
}
async function run(id: number) { runningID.value = id; error.value = ''; message.value = ''; try { await runAdminTask(id); const task = await fetchAdminTask(id); trackAdminTask(task); message.value = `任务 #${id} 已启动。`; await refreshAll(); if (selectedTask.value?.id === id) await refreshSelectedTask(id); window.setTimeout(refreshAll, 1200) } catch (e: any) { error.value = e?.response?.data?.message || '任务启动失败。' } finally { runningID.value = 0 } }

function stopDetailPolling() { if (detailPollTimer !== undefined) { window.clearTimeout(detailPollTimer); detailPollTimer = undefined } }
function scheduleDetailPolling() { stopDetailPolling(); if (selectedTask.value && selectedTask.value.status < 2) detailPollTimer = window.setTimeout(() => void refreshSelectedTask(selectedTask.value?.id || 0), 2000) }
async function refreshSelectedTask(id: number) {
  if (!id) return
  try { const task = await fetchAdminTask(id); if (selectedTask.value?.id !== id) return; selectedTask.value = task; trackAdminTask(task); await loadTaskItems(); scheduleDetailPolling() }
  catch (e: any) { if (selectedTask.value?.id === id) itemError.value = e?.response?.data?.message || '任务详情加载失败。' }
}
async function openTask(task: AdminTask) { invalidateTaskItems(); taskItems.value = []; itemTotal.value = 0; selectedTask.value = task; itemOffset.value = 0; itemStatusFilter.value = ''; await syncURL(); await refreshSelectedTask(task.id) }
async function openTaskByID(id: number, replace = false) { invalidateTaskItems(); taskItems.value = []; itemTotal.value = 0; try { selectedTask.value = await fetchAdminTask(id); itemOffset.value = 0; itemStatusFilter.value = ''; if (replace) await syncURL(true); await loadTaskItems(); scheduleDetailPolling() } catch (e: any) { error.value = e?.response?.data?.message || '任务详情加载失败。'; selectedTask.value = null } }
async function closeTask() { stopDetailPolling(); invalidateTaskItems(); selectedTask.value = null; taskItems.value = []; itemTotal.value = 0; await syncURL() }
async function applyItemFilter() { itemOffset.value = 0; await loadTaskItems() }
async function changeItemPage(value: { offset: number; limit: number }) { itemOffset.value = value.offset; itemLimit.value = value.limit; await loadTaskItems() }

watch(() => route.fullPath, async () => {
  const nextType = String(route.query.type || ''), nextStatus = String(route.query.status || '')
  const rawLimit = Number(route.query.limit), nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  if (nextType !== typeFilter.value || nextStatus !== statusFilter.value || nextLimit !== limit.value || nextOffset !== offset.value) {
    typeFilter.value = nextType; statusFilter.value = nextStatus; limit.value = nextLimit; offset.value = nextOffset; await load()
  }
  const taskID = Number(route.query.task) || 0
  if (!taskID && selectedTask.value) { stopDetailPolling(); invalidateTaskItems(); selectedTask.value = null; taskItems.value = []; itemTotal.value = 0 }
  else if (taskID && selectedTask.value?.id !== taskID) await openTaskByID(taskID)
})
onMounted(async () => { await refreshAll(); const taskID = Number(route.query.task) || 0; if (taskID) await openTaskByID(taskID); if (route.query.create === 'email') { await openCreate(); form.type = 'email'; createState.markClean() } overviewPollTimer = window.setInterval(() => { if ((summary.value?.pending || 0) + (summary.value?.running || 0) > 0) void refreshAll() }, 5000) })
onBeforeUnmount(() => { stopDetailPolling(); if (overviewPollTimer !== undefined) window.clearInterval(overviewPollTimer) })
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.error-count{color:var(--danger);font-size:10px;font-weight:700}.form-section { display: grid; gap: 14px; padding: 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.form-section > div:first-child strong { font-size: 13px; }.form-section > div:first-child p { margin: 3px 0 0; color: var(--muted); font-size: 11px; }.auto-run { align-self: end; min-height: 40px; }.task-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.task-facts>div{display:grid;gap:5px;padding:12px;border:1px solid var(--line);border-radius:9px;background:var(--surface-soft)}.task-facts span{color:var(--muted);font-size:9px}.task-facts strong{font-size:11px}.task-items-section{display:grid;gap:12px}.task-items-section>header{display:flex;align-items:flex-end;justify-content:space-between;gap:12px}.task-items-section h3{margin:0;font-size:13px}.task-items-section p{margin:3px 0 0;color:var(--muted);font-size:9px}:deep(.task-item-table .output-block){max-width:360px}@media(max-width:680px){.task-facts{grid-template-columns:1fr}.task-items-section>header{align-items:stretch;flex-direction:column}}
.task-overview{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px}.task-overview-card,.task-overview-progress{display:grid;gap:5px;padding:13px;border:1px solid var(--line);border-radius:10px;background:var(--surface);text-align:left}.task-overview-card{color:inherit;font:inherit;cursor:pointer}.task-overview-card:hover,.task-overview-card.active{border-color:var(--primary);background:var(--primary-soft)}.task-overview-card span,.task-overview-progress span{color:var(--muted);font-size:9px}.task-overview-card strong{font-size:20px}.task-overview-card small,.task-overview-progress small{color:var(--muted);font-size:9px}.task-overview-progress{grid-column:1/-1}.task-overview-progress>div:first-child{display:flex;align-items:center;justify-content:space-between;gap:12px}.progress-track,.row-progress>span{height:6px;overflow:hidden;border-radius:999px;background:var(--surface-soft)}.progress-track i,.row-progress i{height:100%;display:block;border-radius:inherit;background:var(--primary)}.row-progress{min-width:72px;display:grid;gap:5px}.row-progress>span{width:72px}@media(max-width:900px){.task-overview{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:520px){.task-overview{grid-template-columns:1fr}}
.toolbar .p-select { width: 150px; min-height: 36px; }
</style>
