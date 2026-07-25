<template>
  <section class="standard-page" :class="{ 'detail-active': Boolean(selectedTicketID) }">
    <PageHeader
      :title="admin ? '工单中心' : '我的工单'"
      :description="admin ? '集中处理用户问题，所有回复与状态变化都会保留在时间线中。' : '提交连接、账单或账户问题，并在同一条时间线中持续追问。'"
      eyebrow="Support"
    >
      <template #actions>
        <PageRefreshButton label="刷新工单" :loading="loading" @click="loadTickets()" />
        <UiButton v-if="!admin" type="button" @click="openCreate"><UiIcon name="plus" />新建工单</UiButton>
      </template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="工单操作已完成" error-title="工单操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
          <WorkbenchFilterBar :active="Boolean(filters.q || filters.status || filters.category)" @clear="clearFilters">
            <WorkbenchFilterInput v-model="filters.q" label="搜索" :placeholder="admin ? '编号、主题或用户邮箱' : '编号或主题'" @apply="applyFilters" />
            <WorkbenchFilterSelect v-model="filters.status" label="工单状态" :options="statusFilterOptions" @apply="applyFilters" />
            <WorkbenchFilterSelect v-model="filters.category" label="问题分类" :options="categoryFilterOptions" @apply="applyFilters" />
          </WorkbenchFilterBar>
      </template>
      <template #actions><span class="muted">按最近活动排序</span></template>

      <div class="ticket-workspace" :class="{ 'detail-open': Boolean(selectedTicketID) }">
      <article class="panel ticket-list-panel">
        <header class="panel-header"><div><h2>{{ admin ? '处理队列' : '工单记录' }}</h2><p>选择一条工单查看完整沟通记录。</p></div><span class="count-label">本页 {{ tickets.length }} 条</span></header>
        <div v-if="tickets.length" class="ticket-list">
          <UiButton
            v-for="ticket in tickets"
            :key="ticket.id"
            variant="ghost"
            type="button"
            :class="{ active: selectedTicketID === ticket.id }"
            :aria-current="selectedTicketID === ticket.id ? 'true' : undefined"
            :data-ticket-trigger="ticket.id"
            @click="selectTicket(ticket.id)"
          >
            <div class="ticket-list-head"><code>{{ ticket.ticket_no }}</code><StatusBadge :tone="statusTone(ticket.status)">{{ statusLabel(ticket.status) }}</StatusBadge></div>
            <strong>{{ ticket.subject }}</strong>
            <p v-if="admin">{{ ticket.user_email }}</p>
            <div class="ticket-list-meta"><span>{{ categoryLabel(ticket.category) }}</span><span v-if="ticket.priority === 2" class="urgent">紧急</span><TimeBadge :value="ticket.last_message_at" /></div>
          </UiButton>
        </div>
        <EmptyState v-else icon="ticket" title="没有匹配工单" :description="admin ? '当前筛选条件下没有待处理记录。' : '遇到问题时，可以创建第一张工单。'">
          <template v-if="!admin" #actions><UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建工单</UiButton></template>
        </EmptyState>
      </article>

      <article class="panel ticket-detail-panel">
        <div v-if="detailLoading && !detail" class="ticket-detail-loading" role="status" aria-live="polite">
          <StatusBadge tone="info" icon="refresh">正在加载工单详情</StatusBadge>
        </div>
        <PageAlert v-else-if="detailError && !detail" class="ticket-detail-error" tone="danger" title="工单详情加载失败">
          {{ detailError }}
          <template #actions><UiButton variant="secondary" size="sm" type="button" @click="retryDetail">重试</UiButton></template>
        </PageAlert>
        <template v-else-if="detail">
          <PageAlert v-if="detailError" class="ticket-detail-error" tone="danger" title="工单详情未完全加载">
            {{ detailError }}
            <template #actions><UiButton variant="secondary" size="sm" type="button" @click="retryDetail">重新加载</UiButton></template>
          </PageAlert>
          <header class="ticket-detail-header">
            <div>
              <UiButton class="ticket-back-button" variant="ghost" size="sm" type="button" @click="closeDetail">
                <UiIcon name="chevron" />返回列表
              </UiButton>
              <div class="ticket-title-line"><code>{{ detail.ticket.ticket_no }}</code><StatusBadge :tone="statusTone(detail.ticket.status)">{{ statusLabel(detail.ticket.status) }}</StatusBadge><span v-if="detail.ticket.priority === 2" class="priority-badge">紧急</span><TimeBadge :value="detail.ticket.created_at" /></div><h2>{{ detail.ticket.subject }}</h2><p>{{ categoryLabel(detail.ticket.category) }}<template v-if="admin"> · {{ detail.ticket.user_email }}</template></p>
            </div>
            <div class="ticket-detail-actions">
              <label v-if="admin" class="status-select"><span>流转状态</span><UiSelect aria-label="流转状态" :model-value="selectedStatus" :options="statusOptions" :disabled="saving" @update:model-value="changeStatus($event as TicketStatus)" /></label>
              <UiButton v-else-if="detail.ticket.status !== 'closed'" variant="secondary" size="sm" type="button" :disabled="saving" @click="closeCurrent">关闭工单</UiButton>
            </div>
          </header>

          <div ref="timelineElement" class="ticket-timeline">
            <div v-if="detail.has_older_messages" class="timeline-history-control">
              <UiButton variant="secondary" size="sm" type="button" :loading="loadingOlder" @click="loadOlderMessages">
                加载更早记录
              </UiButton>
              <span>当前显示 {{ detail.messages.length }} / {{ detail.ticket.message_count }} 条</span>
            </div>
            <template v-for="item in detail.messages" :key="item.id">
              <div v-if="item.type === 'status'" class="timeline-event"><span></span><p>{{ roleLabel(item.author_role) }}将状态从“{{ statusLabel(item.from_status) }}”改为“{{ statusLabel(item.to_status) }}”</p><TimeBadge :value="item.created_at" /></div>
              <article v-else class="ticket-message" :class="item.author_role">
                <header><span class="message-avatar">{{ item.author_role === 'admin' ? 'A' : 'U' }}</span><div><strong>{{ item.author_role === 'admin' ? '管理员' : (admin ? item.author_email : '我') }}</strong><TimeBadge :value="item.created_at" mode="exact" /></div></header>
                <p>{{ item.body }}</p>
              </article>
            </template>
          </div>

          <form v-if="detail.ticket.status !== 'closed'" ref="replyFormElement" class="ticket-reply" novalidate @submit.prevent="sendReply">
            <PageAlert v-if="replyErrors.formError.value" tone="danger" title="回复未发送">{{ replyErrors.formError.value }}</PageAlert>
            <FormField v-slot="{ controlAttrs }" :label="admin ? '回复用户' : detail.ticket.status === 'resolved' ? '继续追问（将重新打开工单）' : '补充信息'" name="ticket-reply-body" :error="replyErrors.fields.body" required><UiTextarea v-model="replyBody" v-bind="controlAttrs" maxlength="5000" placeholder="清楚描述处理结果、复现信息或需要补充的内容…" /></FormField>
            <footer><small>{{ replyBody.length }} / 5000</small><UiButton type="submit" :loading="saving" :disabled="!replyBody.trim()">发送回复</UiButton></footer>
          </form>
          <div v-else class="closed-notice"><UiIcon name="check" /><div><strong>工单已关闭</strong><p>时间线保持只读。如有新问题，请创建新的工单。</p></div></div>
        </template>
        <div v-else class="ticket-detail-empty"><span><UiIcon name="ticket" /></span><h2>选择一条工单</h2><p>这里会按需展示问题详情、回复和状态流转记录。</p></div>
      </article>
      </div>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <ModalDialog :open="createOpen" :dirty="createState.dirty.value" title="新建工单" description="请尽量一次性提供问题现象和复现信息。" size="lg" :busy="saving" @close="createOpen = false">
      <form id="create-ticket-form" ref="createFormElement" class="form-grid" novalidate @submit.prevent="submitTicket">
        <PageAlert v-if="createErrors.formError.value" class="field-full" tone="danger" title="无法提交工单">{{ createErrors.formError.value }}</PageAlert>
        <FormField v-slot="{ controlAttrs }" label="问题主题" name="ticket-subject" :error="createErrors.fields.subject" required full><UiInput v-model.trim="draft.subject" v-bind="controlAttrs" maxlength="160" placeholder="例如：订阅导入后节点连接超时" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="问题分类" name="ticket-category" :error="createErrors.fields.category"><UiSelect v-model="draft.category" v-bind="controlAttrs" :options="categoryOptions" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="优先级" name="ticket-priority" :error="createErrors.fields.priority"><UiSelect v-model.number="draft.priority" v-bind="controlAttrs" :options="priorityOptions" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="问题描述" name="ticket-body" :error="createErrors.fields.body" hint="请勿提交密码、订阅凭证等敏感信息。" required full><UiTextarea v-model.trim="draft.body" v-bind="controlAttrs" maxlength="5000" placeholder="操作步骤、错误提示、发生时间，以及你已经尝试过的方法。" /></FormField>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="create-ticket-form" type="submit" :loading="saving">提交工单</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { closeTicket, createTicket, fetchTicket, fetchTickets, replyTicket, updateTicketStatus, type Ticket, type TicketCategory, type TicketDetail, type TicketStatus } from '../api/client'
import { confirmAction } from '../utils/feedback'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { formatUnknownValue } from '../utils/format'
import { preserveAdminReturnTo } from '../utils/navigation'
import { collectFieldErrors, isCharacterLengthInRange, isOneOf } from '../utils/validation'
import DataWorkbench from './DataWorkbench.vue'
import EmptyState from './EmptyState.vue'
import FormField from './FormField.vue'
import ModalDialog from './ModalDialog.vue'
import PageAlert from './PageAlert.vue'
import PageHeader from './PageHeader.vue'
import StatusBadge from './StatusBadge.vue'
import TablePager from './TablePager.vue'
import TransientFeedback from './TransientFeedback.vue'
import UiIcon from './UiIcon.vue'
import WorkbenchFilterBar from './WorkbenchFilterBar.vue'
import WorkbenchFilterInput from './WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from './WorkbenchFilterSelect.vue'

const props = defineProps<{ admin?: boolean }>()
const route = useRoute()
const router = useRouter()
const admin = computed(() => Boolean(props.admin))
const detail = ref<TicketDetail | null>(null)
const selectedStatus = ref<TicketStatus>('open')
const selectedTicketID = ref(validTicketID(route.query.ticket))
const detailLoading = ref(false)
const detailError = ref('')
const loadingOlder = ref(false)
const saving = ref(false)
const message = ref('')
const createOpen = ref(false)
const replyBody = ref('')
const createFormElement = ref<HTMLElement | null>(null)
const replyFormElement = ref<HTMLElement | null>(null)
const createErrors = useFormErrors()
const replyErrors = useFormErrors()
const timelineElement = ref<HTMLElement | null>(null)
const filters = reactive({ q: String(route.query.q || ''), status: String(route.query.status || ''), category: String(route.query.category || '') })
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 25)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const draft = reactive<{ subject: string; category: TicketCategory; priority: 1 | 2; body: string }>({ subject: '', category: 'connection', priority: 1, body: '' })
const createState = useDirtyForm(() => draft)
const replyState = useDirtyForm(() => replyBody.value)
useUnsavedChangesGuard(
  () => (createOpen.value && createState.dirty.value) || replyState.dirty.value,
  async () => {
    if (createOpen.value && !await createState.confirmDiscard({
      title: '放弃新工单草稿？',
      message: '离开工单中心后，尚未提交的问题描述将丢失。',
      confirmText: '离开页面',
    })) return false
    if (!await replyState.confirmDiscard({
      title: '放弃未发送的回复？',
      message: '离开工单中心后，当前回复草稿将丢失。',
      confirmText: '离开页面',
    })) return false
    return true
  },
)
let detailController: AbortController | null = null

const statusOptions: { value: TicketStatus; label: string }[] = [
  { value: 'open', label: '新工单' }, { value: 'pending_admin', label: '待管理员处理' }, { value: 'pending_user', label: '待用户回复' }, { value: 'resolved', label: '已解决' }, { value: 'closed', label: '已关闭' }
]
const categoryOptions: { value: TicketCategory; label: string }[] = [
  { value: 'connection', label: '连接问题' }, { value: 'billing', label: '账单与套餐' }, { value: 'account', label: '账户问题' }, { value: 'other', label: '其他问题' }
]
const statusFilterOptions = computed(() => [
  { value: '', label: '全部状态' },
  ...(admin.value ? [{ value: 'attention', label: '待处理（新工单 / 待管理员回复）' }] : []),
  ...statusOptions,
])
const categoryFilterOptions = [{ value: '', label: '全部分类' }, ...categoryOptions]
const priorityOptions = [{ value: 1, label: '普通' }, { value: 2, label: '紧急' }]
const ticketCategories = ['connection', 'billing', 'account', 'other'] as const
const ticketPriorities = [1, 2] as const
watch(() => draft.subject, () => createErrors.clear('subject'))
watch(() => draft.category, () => createErrors.clear('category'))
watch(() => draft.priority, () => createErrors.clear('priority'))
watch(() => draft.body, () => createErrors.clear('body'))
watch(replyBody, () => replyErrors.clear('body'))
const { items: tickets, total, loading, refreshing, error, load: loadTicketPage } = useRemoteTable<Ticket>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchTickets({ status: filters.status || undefined, category: filters.category || undefined, q: filters.q || undefined, offset: offset.value, limit: limit.value }, admin.value, { signal }),
  errorMessage: (cause: any) => apiError(cause, '工单列表加载失败。'),
  onOffsetCorrected: () => syncURL(true),
})
function statusLabel(status: string) { return statusOptions.find(item => item.value === status)?.label || `未知状态（${status || '空'}）` }
function categoryLabel(category: string) { return categoryOptions.find(item => item.value === category)?.label || formatUnknownValue('分类', category) }
function roleLabel(role: string) { return role === 'admin' ? '管理员' : role === 'user' ? '用户' : '系统' }
function statusTone(status: string): 'neutral' | 'info' | 'warning' | 'success' { return status === 'resolved' || status === 'closed' ? 'success' : status === 'pending_admin' ? 'warning' : status === 'pending_user' ? 'info' : 'neutral' }
function apiError(e: any, fallback: string) { return e?.response?.data?.message || fallback }

async function loadTickets() {
  await loadTicketPage()
}

function validTicketID(value: unknown) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 0
}

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = {
    query: {
      ...preserveAdminReturnTo(route.query.return_to),
      ...(filters.q ? { q: filters.q } : {}),
      ...(filters.status ? { status: filters.status } : {}),
      ...(filters.category ? { category: filters.category } : {}),
      ...(page > 1 ? { page: String(page) } : {}),
      ...(limit.value !== 25 ? { limit: String(limit.value) } : {}),
      ...(selectedTicketID.value ? { ticket: String(selectedTicketID.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
}

function clearDetailState() {
  detailController?.abort()
  detailController = null
  selectedTicketID.value = 0
  detail.value = null
  detailLoading.value = false
  detailError.value = ''
  loadingOlder.value = false
}
async function discardReplyForNavigation(message: string) {
  if (!await replyState.confirmDiscard({ title: '放弃未发送的回复？', message, confirmText: '放弃草稿' })) return false
  replyBody.value = ''; replyErrors.clear(); replyState.markClean(); return true
}

async function applyFilters() {
  if (!await discardReplyForNavigation('应用筛选后，当前回复草稿将被清空。')) {
    Object.assign(filters, {
      q: String(route.query.q || ''),
      status: String(route.query.status || ''),
      category: String(route.query.category || ''),
    })
    return
  }
  offset.value = 0
  clearDetailState()
  await syncURL()
  await loadTickets()
}

async function clearFilters() {
  Object.assign(filters, { q: '', status: '', category: '' })
  await applyFilters()
}

async function changePage(value: { offset: number; limit: number }) {
  if (!await discardReplyForNavigation('切换列表页后，当前回复草稿将被清空。')) return
  offset.value = value.offset
  limit.value = value.limit
  clearDetailState()
  await syncURL()
  await loadTickets()
}

async function openCreate() {
  if (!await discardReplyForNavigation('新建工单后，当前回复草稿将被清空。')) return
  Object.assign(draft, { subject: '', category: 'connection', priority: 1, body: '' })
  createErrors.clear(); createState.markClean(); createOpen.value = true
}

async function selectTicket(id: number) {
  if (id === selectedTicketID.value && detail.value) return
  if (id !== selectedTicketID.value && !await discardReplyForNavigation('切换工单后，当前回复草稿将被清空。')) return
  selectedTicketID.value = id
  detail.value = null
  detailError.value = ''
  await syncURL()
  await loadTicketDetail(id)
}

async function loadTicketDetail(id: number) {
  detailController?.abort()
  detailController = new AbortController()
  const current = detailController
  selectedTicketID.value = id
  detailLoading.value = true
  detailError.value = ''
  try {
    const result = await fetchTicket(id, admin.value, { signal: current.signal, messageLimit: 100 })
    if (current.signal.aborted || selectedTicketID.value !== id) return
    detail.value = result
    await scrollTimeline()
  } catch (e: any) {
    if (!current.signal.aborted && selectedTicketID.value === id) detailError.value = apiError(e, '工单详情加载失败。')
  } finally {
    if (selectedTicketID.value === id) detailLoading.value = false
  }
}

async function closeDetail() {
  if (!selectedTicketID.value) return
  if (!await discardReplyForNavigation('返回工单列表后，当前回复草稿将被清空。')) return
  const previousID = selectedTicketID.value
  clearDetailState()
  await syncURL()
  await nextTick()
  document.querySelector<HTMLElement>(`[data-ticket-trigger="${previousID}"]`)?.focus()
}

async function retryDetail() {
  if (selectedTicketID.value) await loadTicketDetail(selectedTicketID.value)
}

async function loadOlderMessages() {
  if (!detail.value?.has_older_messages || !detail.value.oldest_message_id || loadingOlder.value) return
  const ticketID = detail.value.ticket.id
  const beforeID = detail.value.oldest_message_id
  const timeline = timelineElement.value
  const previousHeight = timeline?.scrollHeight || 0
  const previousTop = timeline?.scrollTop || 0
  loadingOlder.value = true
  detailError.value = ''
  try {
    const older = await fetchTicket(ticketID, admin.value, { beforeId: beforeID, messageLimit: 100 })
    if (selectedTicketID.value !== ticketID || !detail.value) return
    const seen = new Set(detail.value.messages.map(item => item.id))
    detail.value = {
      ...detail.value,
      ticket: older.ticket,
      messages: [...older.messages.filter(item => !seen.has(item.id)), ...detail.value.messages],
      has_older_messages: older.has_older_messages,
      oldest_message_id: older.oldest_message_id,
    }
    await nextTick()
    if (timeline) timeline.scrollTop = previousTop + (timeline.scrollHeight - previousHeight)
  } catch (e: any) {
    if (selectedTicketID.value === ticketID) detailError.value = apiError(e, '更早的工单记录加载失败。')
  } finally {
    loadingOlder.value = false
  }
}

async function submitTicket() {
  draft.subject = draft.subject.trim()
  draft.body = draft.body.trim()
  const valid = await createErrors.applyValidation(collectFieldErrors({
    subject: !isCharacterLengthInRange(draft.subject, 1, 160, true) && '问题主题需包含 1 到 160 个字符。',
    category: !isOneOf(draft.category, ticketCategories) && '请选择有效的问题分类。',
    priority: !isOneOf(draft.priority, ticketPriorities) && '请选择有效的优先级。',
    body: !isCharacterLengthInRange(draft.body, 1, 5000, true) && '问题描述需包含 1 到 5000 个字符。',
  }), createFormElement, '请更正标记字段后再提交工单。')
  if (!valid) return
  saving.value = true; message.value = ''
  try {
    const created = await createTicket({ ...draft })
    createOpen.value = false; Object.assign(draft, { subject: '', category: 'connection', priority: 1, body: '' })
    createState.markClean()
    selectedTicketID.value = created.ticket.id
    detail.value = created
    message.value = `工单 ${created.ticket.ticket_no} 已提交。`
    await syncURL()
    await loadTickets()
    await scrollTimeline()
  } catch (e: any) { await createErrors.applyApiError(e, '工单提交失败。', createFormElement, { subject: 'subject', category: 'category', priority: 'priority', message: 'body', body: 'body' }) }
  finally { saving.value = false }
}

async function sendReply() {
  if (!detail.value) return
  replyBody.value = replyBody.value.trim()
  const valid = await replyErrors.applyValidation(collectFieldErrors({
    body: !isCharacterLengthInRange(replyBody.value, 1, 5000, true) && '回复需包含 1 到 5000 个字符。',
  }), replyFormElement, '请更正标记字段后再发送回复。')
  if (!valid) return
  saving.value = true; message.value = ''
  try {
    detail.value = await replyTicket(detail.value.ticket.id, replyBody.value.trim(), admin.value); replyBody.value = ''; replyState.markClean()
    message.value = '回复已写入工单时间线。'; await loadTickets(); await scrollTimeline()
  } catch (e: any) { await replyErrors.applyApiError(e, '回复发送失败。', replyFormElement, { message: 'body', body: 'body' }) }
  finally { saving.value = false }
}

async function changeStatus(status: TicketStatus) {
  if (!detail.value) return
  const currentStatus = detail.value.ticket.status
  if (status === currentStatus) {
    selectedStatus.value = currentStatus
    return
  }
  selectedStatus.value = status
  if (status === 'closed') {
    const confirmed = await confirmAction({
      title: '关闭工单',
      message: '关闭后用户与管理员都不能继续回复；历史消息和状态时间线会保留。',
      confirmText: '确认关闭',
      tone: 'danger',
    })
    if (!confirmed) {
      selectedStatus.value = currentStatus
      return
    }
  }
  saving.value = true; error.value = ''; message.value = ''
  try {
    detail.value = await updateTicketStatus(detail.value.ticket.id, status)
    selectedStatus.value = detail.value.ticket.status
    message.value = `工单状态已更新为“${statusLabel(status)}”。`
    await loadTickets()
    await scrollTimeline()
  } catch (e: any) {
    selectedStatus.value = currentStatus
    error.value = apiError(e, '状态更新失败。')
  }
  finally { saving.value = false }
}

async function closeCurrent() {
  if (!detail.value || !await confirmAction({ title: '关闭工单', message: '关闭后将不能继续回复；历史消息和处理时间线会保留。', confirmText: '确认关闭', tone: 'danger' })) return
  saving.value = true; error.value = ''; message.value = ''
  try { detail.value = await closeTicket(detail.value.ticket.id); message.value = '工单已关闭。'; await loadTickets(); await scrollTimeline() }
  catch (e: any) { error.value = apiError(e, '工单关闭失败。') }
  finally { saving.value = false }
}

async function scrollTimeline() { await nextTick(); if (timelineElement.value) timelineElement.value.scrollTop = timelineElement.value.scrollHeight }
watch(() => detail.value?.ticket.status, (status) => {
  if (status) selectedStatus.value = status
})
watch(() => route.fullPath, async () => {
  const nextFilters = {
    q: String(route.query.q || ''),
    status: String(route.query.status || ''),
    category: String(route.query.category || ''),
  }
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 25
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  const nextTicket = validTicketID(route.query.ticket)
  const listChanged = nextFilters.q !== filters.q
    || nextFilters.status !== filters.status
    || nextFilters.category !== filters.category
    || nextLimit !== limit.value
    || nextOffset !== offset.value
  if (listChanged) {
    Object.assign(filters, nextFilters)
    limit.value = nextLimit
    offset.value = nextOffset
    await loadTickets()
  }
  if (!nextTicket && selectedTicketID.value) {
    const previousID = selectedTicketID.value
    clearDetailState()
    await nextTick()
    document.querySelector<HTMLElement>(`[data-ticket-trigger="${previousID}"]`)?.focus()
  } else if (nextTicket && nextTicket !== selectedTicketID.value) {
    selectedTicketID.value = nextTicket
    detail.value = null
    await loadTicketDetail(nextTicket)
  }
})

onBeforeRouteUpdate(async (to) => {
  if (!replyState.dirty.value) return true
  const nextTicket = validTicketID(to.query.ticket)
  const currentTicket = selectedTicketID.value
  const listContextChanged = String(to.query.q || '') !== filters.q
    || String(to.query.status || '') !== filters.status
    || String(to.query.category || '') !== filters.category
    || Number(to.query.page || 1) !== Math.floor(offset.value / limit.value) + 1
    || Number(to.query.limit || 25) !== limit.value
  if (nextTicket === currentTicket && !listContextChanged) return true
  return discardReplyForNavigation('切换工单或列表状态后，当前回复草稿将被清空。')
})

onMounted(async () => {
  await loadTickets()
  const initialTicket = validTicketID(route.query.ticket)
  if (initialTicket) await loadTicketDetail(initialTicket)
})
onBeforeUnmount(() => detailController?.abort())
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }
.ticket-summary { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; margin-bottom: 16px; }
.ticket-summary article { display: flex; align-items: center; gap: 14px; padding: 17px 18px; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--surface); }
.ticket-summary article > div { display: grid; gap: 2px; }.ticket-summary small,.ticket-summary p { color: var(--muted); font-size: 10px; }.ticket-summary strong { font-size: 23px; }.ticket-summary p { margin: 0; }
.ticket-toolbar { margin-bottom: 16px; }.ticket-toolbar .panel-body { margin: 0; padding: 12px 14px; }.ticket-toolbar select { width: 160px; min-height: 36px; }
.ticket-workspace { min-height: 650px; display: grid; grid-template-columns: minmax(300px, .38fr) minmax(480px, .62fr); gap: 16px; }
.ticket-list-panel,.ticket-detail-panel { min-width: 0; overflow: hidden; }.ticket-list { max-height: 690px; overflow-y: auto; }
.ticket-list > button { width: 100%; display: grid; gap: 8px; padding: 16px 18px; text-align: left; border: 0; border-bottom: 1px solid var(--line); background: var(--surface); }
.ticket-list > button:hover,.ticket-list > button.active { background: var(--surface-selected); }.ticket-list > button.active { box-shadow: inset 3px 0 var(--primary); }
.ticket-list-head,.ticket-list-meta,.ticket-title-line { display: flex; align-items: center; gap: 8px; }.ticket-list-head { justify-content: space-between; }.ticket-list code,.ticket-detail-header code { color: var(--primary); font-size: 10px; font-weight: 700; }.ticket-list > button > strong { overflow: hidden; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }.ticket-list > button > p { margin: -3px 0 0; overflow: hidden; color: var(--muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.ticket-list-meta { color: var(--muted); font-size: 9px; }.ticket-list-meta span:last-child { margin-left: auto; }.urgent,.priority-badge { color: var(--danger); font-weight: 700; }.ticket-state { padding: 50px 20px; color: var(--muted); text-align: center; }
.ticket-detail-panel { display: flex; flex-direction: column; }.ticket-detail-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; padding: 20px; border-bottom: 1px solid var(--line); }.ticket-detail-header h2 { margin: 10px 0 5px; font-size: 19px; }.ticket-detail-header p { margin: 0; color: var(--muted); font-size: 10px; }.ticket-detail-actions { flex: 0 0 auto; }.status-select { display: grid; gap: 4px; }.status-select span { color: var(--muted); font-size: 9px; }.status-select select { min-width: 150px; min-height: 34px; padding-block: 5px; font-size: 11px; }.priority-badge { padding: 3px 7px; border-radius: 999px; background: var(--danger-soft); font-size: 9px; }
.ticket-detail-loading { min-height: 420px; display: grid; place-items: center; }
.ticket-detail-error { margin: 16px 20px; }
.ticket-back-button { display: none; margin-bottom: 10px; }
.ticket-back-button :deep(.ui-icon) { transform: rotate(180deg); }
.ticket-timeline { flex: 1; min-height: 340px; max-height: 520px; display: grid; align-content: start; gap: 14px; overflow-y: auto; padding: 22px; background: var(--surface-canvas); }.ticket-message { max-width: 78%; display: grid; gap: 10px; justify-self: start; padding: 13px 15px; border: 1px solid var(--line); border-radius: 4px 13px 13px 13px; background: var(--surface); }.ticket-message.admin { justify-self: end; border-color: var(--info-border-strong); border-radius: 13px 4px 13px 13px; background: var(--info-soft-alt); }.ticket-message header { display: flex; align-items: center; gap: 8px; }.message-avatar { width: 25px; height: 25px; display: grid; place-items: center; border-radius: 50%; color: var(--text-inverse); background: var(--neutral-avatar); font-size: 9px; font-weight: 750; }.ticket-message.admin .message-avatar { background: var(--primary); }.ticket-message header div { display: grid; gap: 1px; }.ticket-message strong { font-size: 10px; }.ticket-message time { color: var(--muted); font-size: 8px; }.ticket-message > p { margin: 0; color: var(--text-body); font-size: 12px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }.timeline-event { display: flex; align-items: center; justify-content: center; gap: 7px; }.timeline-event > span { width: 6px; height: 6px; border-radius: 50%; background: var(--neutral-icon); }.timeline-event p { margin: 0; color: var(--muted); font-size: 9px; }
.timeline-history-control { display: flex; align-items: center; justify-content: center; gap: 10px; color: var(--muted); font-size: 9px; }
.ticket-reply { display: grid; gap: 10px; padding: 16px 20px; border-top: 1px solid var(--line); }.ticket-reply textarea { min-height: 90px; }.ticket-reply footer { display: flex; align-items: center; justify-content: space-between; }.ticket-reply small { color: var(--muted); font-size: 9px; }.closed-notice { display: flex; align-items: center; gap: 11px; padding: 18px 20px; border-top: 1px solid var(--line); background: var(--success-soft); }.closed-notice > .ui-icon { color: var(--success); font-size: 20px; }.closed-notice strong { font-size: 12px; }.closed-notice p { margin: 2px 0 0; color: var(--muted); font-size: 10px; }.ticket-detail-empty { min-height: 600px; display: grid; place-items: center; align-content: center; text-align: center; }.ticket-detail-empty > span { width: 52px; height: 52px; display: grid; place-items: center; border-radius: 14px; color: var(--primary); background: var(--primary-soft); font-size: 23px; }.ticket-detail-empty h2 { margin: 14px 0 5px; font-size: 17px; }.ticket-detail-empty p { color: var(--muted); font-size: 11px; }
@media (max-width: 1000px) { .ticket-workspace { grid-template-columns: 1fr; }.ticket-list { max-height: 360px; }.ticket-detail-empty { min-height: 320px; } }
@media (max-width: 680px) {
  .standard-page.detail-active { gap: 0; }
  .standard-page.detail-active > .page-header,
  .standard-page.detail-active :deep(.workbench-toolbar) { display: none; }
  .ticket-summary { grid-template-columns: 1fr; }
  .ticket-workspace { min-height: 0; display: block; }
  .ticket-workspace.detail-open .ticket-list-panel { display: none; }
  .ticket-workspace:not(.detail-open) .ticket-detail-panel { display: none; }
  .ticket-detail-header { display: grid; padding: 16px; }
  .ticket-title-line { flex-wrap: wrap; }
  .ticket-title-line code,
  .ticket-title-line :deep(.status-badge),
  .ticket-title-line :deep(.time-badge) { flex: 0 0 auto; white-space: nowrap; }
  .ticket-detail-actions,
  .status-select,
  .status-select :deep(.p-select) { width: 100%; }
  .ticket-back-button { display: inline-flex; }
  .ticket-timeline { min-height: 300px; max-height: 48vh; padding: 16px; }
  .ticket-message { max-width: 92%; }
  .ticket-reply { padding: 14px 16px; }
  .ticket-toolbar .toolbar-group,.ticket-toolbar select { width: 100%; }
  .timeline-history-control { align-items: stretch; flex-direction: column; text-align: center; }
}
.ticket-toolbar .p-select { width: 160px; min-height: 36px; }.status-select .p-select { min-width: 150px; min-height: 34px; font-size: 11px; }@media (max-width: 680px) { .ticket-toolbar .p-select { width: 100%; } }
.ticket-list>button{color:var(--text)}.ticket-list>button:hover,.ticket-list>button.active{background:var(--primary-soft)}.ticket-message.admin{border-color:var(--primary-border);background:var(--primary-soft)}
.ticket-list-meta .time-badge{margin-left:auto}.timeline-event .time-badge{flex:0 0 auto}.ticket-list-panel>.table-pager{border-top:1px solid var(--line)}
</style>
