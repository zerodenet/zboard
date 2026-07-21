<template>
  <section>
    <PageHeader
      :title="admin ? '工单中心' : '我的工单'"
      :description="admin ? '集中处理用户问题，所有回复与状态变化都会保留在时间线中。' : '提交连接、账单或账户问题，并在同一条时间线中持续追问。'"
      eyebrow="Support"
    >
      <template #actions>
        <button class="button button-secondary" type="button" :disabled="loading" @click="loadTickets()"><UiIcon name="refresh" />刷新</button>
        <button v-if="!admin" class="button" type="button" @click="createOpen = true"><UiIcon name="plus" />新建工单</button>
      </template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="ticket-summary">
      <article><span class="metric-icon"><UiIcon name="ticket" /></span><div><small>当前列表</small><strong>{{ total }}</strong><p>{{ admin ? '全部可见工单' : '我的历史工单' }}</p></div></article>
      <article><span class="metric-icon warning"><UiIcon name="clock" /></span><div><small>{{ admin ? '等待管理员' : '等待我回复' }}</small><strong>{{ actionCount }}</strong><p>需要优先处理</p></div></article>
      <article><span class="metric-icon success"><UiIcon name="check" /></span><div><small>已处理</small><strong>{{ doneCount }}</strong><p>已解决或已关闭</p></div></article>
    </div>

    <article class="panel ticket-toolbar">
      <div class="panel-body toolbar">
        <div class="toolbar-group">
          <label class="search-field"><UiIcon name="search" /><input v-model.trim="filters.q" :placeholder="admin ? '搜索编号、主题或用户邮箱' : '搜索编号或主题'" @keyup.enter="loadTickets()" /></label>
          <select v-model="filters.status" aria-label="工单状态" @change="loadTickets()"><option value="">全部状态</option><option v-for="item in statusOptions" :key="item.value" :value="item.value">{{ item.label }}</option></select>
          <select v-model="filters.category" aria-label="问题分类" @change="loadTickets()"><option value="">全部分类</option><option v-for="item in categoryOptions" :key="item.value" :value="item.value">{{ item.label }}</option></select>
          <button class="button button-secondary button-sm" type="button" @click="loadTickets()">查询</button>
        </div>
        <span class="muted">按最近活动排序</span>
      </div>
    </article>

    <div class="ticket-workspace">
      <article class="panel ticket-list-panel">
        <header class="panel-header"><div><h2>{{ admin ? '处理队列' : '工单记录' }}</h2><p>选择一条工单查看完整沟通记录。</p></div><span class="count-label">{{ tickets.length }} 条</span></header>
        <div v-if="loading" class="ticket-state">正在加载工单…</div>
        <div v-else-if="tickets.length" class="ticket-list">
          <button v-for="ticket in tickets" :key="ticket.id" type="button" :class="{ active: detail?.ticket.id === ticket.id }" @click="selectTicket(ticket.id)">
            <div class="ticket-list-head"><code>{{ ticket.ticket_no }}</code><StatusBadge :tone="statusTone(ticket.status)">{{ statusLabel(ticket.status) }}</StatusBadge></div>
            <strong>{{ ticket.subject }}</strong>
            <p v-if="admin">{{ ticket.user_email }}</p>
            <div class="ticket-list-meta"><span>{{ categoryLabel(ticket.category) }}</span><span v-if="ticket.priority === 2" class="urgent">紧急</span><span>{{ formatDateTime(ticket.last_message_at) }}</span></div>
          </button>
        </div>
        <EmptyState v-else icon="ticket" title="没有匹配工单" :description="admin ? '当前筛选条件下没有待处理记录。' : '遇到问题时，可以创建第一张工单。'">
          <template v-if="!admin" #actions><button class="button" type="button" @click="createOpen = true"><UiIcon name="plus" />新建工单</button></template>
        </EmptyState>
      </article>

      <article class="panel ticket-detail-panel">
        <template v-if="detail">
          <header class="ticket-detail-header">
            <div><div class="ticket-title-line"><code>{{ detail.ticket.ticket_no }}</code><StatusBadge :tone="statusTone(detail.ticket.status)">{{ statusLabel(detail.ticket.status) }}</StatusBadge><span v-if="detail.ticket.priority === 2" class="priority-badge">紧急</span></div><h2>{{ detail.ticket.subject }}</h2><p>{{ categoryLabel(detail.ticket.category) }} · 创建于 {{ formatDateTime(detail.ticket.created_at) }}<template v-if="admin"> · {{ detail.ticket.user_email }}</template></p></div>
            <div class="ticket-detail-actions">
              <label v-if="admin" class="status-select"><span>流转状态</span><select :value="detail.ticket.status" :disabled="saving" @change="changeStatus(($event.target as HTMLSelectElement).value as TicketStatus)"><option v-for="item in statusOptions" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
              <button v-else-if="detail.ticket.status !== 'closed'" class="button button-secondary button-sm" type="button" :disabled="saving" @click="closeCurrent">关闭工单</button>
            </div>
          </header>

          <div ref="timelineElement" class="ticket-timeline">
            <template v-for="item in detail.messages" :key="item.id">
              <div v-if="item.type === 'status'" class="timeline-event"><span></span><p>{{ roleLabel(item.author_role) }}将状态从“{{ statusLabel(item.from_status) }}”改为“{{ statusLabel(item.to_status) }}” · {{ formatDateTime(item.created_at) }}</p></div>
              <article v-else class="ticket-message" :class="item.author_role">
                <header><span class="message-avatar">{{ item.author_role === 'admin' ? 'A' : 'U' }}</span><div><strong>{{ item.author_role === 'admin' ? '管理员' : (admin ? item.author_email : '我') }}</strong><time>{{ formatDateTime(item.created_at) }}</time></div></header>
                <p>{{ item.body }}</p>
              </article>
            </template>
          </div>

          <form v-if="detail.ticket.status !== 'closed'" class="ticket-reply" @submit.prevent="sendReply">
            <label class="field"><span>{{ admin ? '回复用户' : detail.ticket.status === 'resolved' ? '继续追问（将重新打开工单）' : '补充信息' }}</span><textarea v-model="replyBody" maxlength="5000" required placeholder="清楚描述处理结果、复现信息或需要补充的内容…"></textarea></label>
            <footer><small>{{ replyBody.length }} / 5000</small><button class="button" type="submit" :disabled="saving || !replyBody.trim()">{{ saving ? '发送中…' : '发送回复' }}</button></footer>
          </form>
          <div v-else class="closed-notice"><UiIcon name="check" /><div><strong>工单已关闭</strong><p>时间线保持只读。如有新问题，请创建新的工单。</p></div></div>
        </template>
        <div v-else class="ticket-detail-empty"><span><UiIcon name="ticket" /></span><h2>选择一条工单</h2><p>这里会展示问题详情、全部回复和状态流转记录。</p></div>
      </article>
    </div>

    <ModalDialog :open="createOpen" title="新建工单" description="请尽量一次性提供问题现象和复现信息。" size="lg" :busy="saving" @close="createOpen = false">
      <form id="create-ticket-form" class="form-grid" @submit.prevent="submitTicket">
        <label class="field field-full"><span>问题主题</span><input v-model.trim="draft.subject" maxlength="160" required placeholder="例如：订阅导入后节点连接超时" /></label>
        <label class="field"><span>问题分类</span><select v-model="draft.category"><option v-for="item in categoryOptions" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
        <label class="field"><span>优先级</span><select v-model.number="draft.priority"><option :value="1">普通</option><option :value="2">紧急</option></select></label>
        <label class="field field-full"><span>问题描述</span><textarea v-model.trim="draft.body" maxlength="5000" required placeholder="操作步骤、错误提示、发生时间，以及你已经尝试过的方法。"></textarea><small class="field-hint">请勿提交密码、订阅凭证等敏感信息。</small></label>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="createOpen = false">取消</button><button class="button" form="create-ticket-form" type="submit" :disabled="saving">{{ saving ? '提交中…' : '提交工单' }}</button></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { closeTicket, createTicket, fetchTicket, fetchTickets, replyTicket, updateTicketStatus, type Ticket, type TicketCategory, type TicketDetail, type TicketStatus } from '../api/client'
import { formatDateTime } from '../utils/format'
import { confirmAction } from '../utils/feedback'
import EmptyState from './EmptyState.vue'
import ModalDialog from './ModalDialog.vue'
import PageHeader from './PageHeader.vue'
import StatusBadge from './StatusBadge.vue'
import UiIcon from './UiIcon.vue'

const props = defineProps<{ admin?: boolean }>()
const admin = computed(() => Boolean(props.admin))
const tickets = ref<Ticket[]>([])
const total = ref(0)
const detail = ref<TicketDetail | null>(null)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const createOpen = ref(false)
const replyBody = ref('')
const timelineElement = ref<HTMLElement | null>(null)
const filters = reactive({ q: '', status: '', category: '' })
const draft = reactive<{ subject: string; category: TicketCategory; priority: 1 | 2; body: string }>({ subject: '', category: 'connection', priority: 1, body: '' })

const statusOptions: { value: TicketStatus; label: string }[] = [
  { value: 'open', label: '新工单' }, { value: 'pending_admin', label: '待管理员处理' }, { value: 'pending_user', label: '待用户回复' }, { value: 'resolved', label: '已解决' }, { value: 'closed', label: '已关闭' }
]
const categoryOptions: { value: TicketCategory; label: string }[] = [
  { value: 'connection', label: '连接问题' }, { value: 'billing', label: '账单与套餐' }, { value: 'account', label: '账户问题' }, { value: 'other', label: '其他问题' }
]
const actionCount = computed(() => tickets.value.filter(item => admin.value ? ['open', 'pending_admin'].includes(item.status) : item.status === 'pending_user').length)
const doneCount = computed(() => tickets.value.filter(item => ['resolved', 'closed'].includes(item.status)).length)

function statusLabel(status: string) { return statusOptions.find(item => item.value === status)?.label || status || '未知状态' }
function categoryLabel(category: string) { return categoryOptions.find(item => item.value === category)?.label || category }
function roleLabel(role: string) { return role === 'admin' ? '管理员' : role === 'user' ? '用户' : '系统' }
function statusTone(status: string): 'neutral' | 'info' | 'warning' | 'success' { return status === 'resolved' || status === 'closed' ? 'success' : status === 'pending_admin' ? 'warning' : status === 'pending_user' ? 'info' : 'neutral' }
function apiError(e: any, fallback: string) { return e?.response?.data?.message || fallback }

async function loadTickets(selectFirst = false) {
  loading.value = true; error.value = ''
  try {
    const result = await fetchTickets({ status: filters.status || undefined, category: filters.category || undefined, q: filters.q || undefined, limit: 100 }, admin.value)
    tickets.value = result.items || []; total.value = result.total || 0
    if (detail.value && !tickets.value.some(item => item.id === detail.value?.ticket.id)) detail.value = null
    if ((selectFirst || !detail.value) && tickets.value.length) await selectTicket(tickets.value[0].id)
  } catch (e: any) { error.value = apiError(e, '工单列表加载失败。') }
  finally { loading.value = false }
}

async function selectTicket(id: number) {
  error.value = ''
  try { detail.value = await fetchTicket(id, admin.value); await scrollTimeline() }
  catch (e: any) { error.value = apiError(e, '工单详情加载失败。') }
}

async function submitTicket() {
  saving.value = true; error.value = ''; message.value = ''
  try {
    const created = await createTicket({ ...draft })
    createOpen.value = false; Object.assign(draft, { subject: '', category: 'connection', priority: 1, body: '' })
    message.value = `工单 ${created.ticket.ticket_no} 已提交。`; await loadTickets(); await selectTicket(created.ticket.id)
  } catch (e: any) { error.value = apiError(e, '工单提交失败。') }
  finally { saving.value = false }
}

async function sendReply() {
  if (!detail.value || !replyBody.value.trim()) return
  saving.value = true; error.value = ''; message.value = ''
  try {
    detail.value = await replyTicket(detail.value.ticket.id, replyBody.value.trim(), admin.value); replyBody.value = ''
    message.value = '回复已写入工单时间线。'; await loadTickets(); await scrollTimeline()
  } catch (e: any) { error.value = apiError(e, '回复发送失败。') }
  finally { saving.value = false }
}

async function changeStatus(status: TicketStatus) {
  if (!detail.value || status === detail.value.ticket.status) return
  saving.value = true; error.value = ''; message.value = ''
  try { detail.value = await updateTicketStatus(detail.value.ticket.id, status); message.value = `工单状态已更新为“${statusLabel(status)}”。`; await loadTickets(); await scrollTimeline() }
  catch (e: any) { error.value = apiError(e, '状态更新失败。') }
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
onMounted(() => loadTickets(true))
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }
.ticket-summary { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; margin-bottom: 16px; }
.ticket-summary article { display: flex; align-items: center; gap: 14px; padding: 17px 18px; border: 1px solid var(--line); border-radius: var(--radius-md); background: #fff; }
.ticket-summary article > div { display: grid; gap: 2px; }.ticket-summary small,.ticket-summary p { color: var(--muted); font-size: 10px; }.ticket-summary strong { font-size: 23px; }.ticket-summary p { margin: 0; }
.ticket-toolbar { margin-bottom: 16px; }.ticket-toolbar .panel-body { margin: 0; padding: 12px 14px; }.ticket-toolbar select { width: 160px; min-height: 36px; }
.ticket-workspace { min-height: 650px; display: grid; grid-template-columns: minmax(300px, .38fr) minmax(480px, .62fr); gap: 16px; }
.ticket-list-panel,.ticket-detail-panel { min-width: 0; overflow: hidden; }.ticket-list { max-height: 690px; overflow-y: auto; }
.ticket-list > button { width: 100%; display: grid; gap: 8px; padding: 16px 18px; text-align: left; border: 0; border-bottom: 1px solid var(--line); background: #fff; }
.ticket-list > button:hover,.ticket-list > button.active { background: #f7faff; }.ticket-list > button.active { box-shadow: inset 3px 0 var(--primary); }
.ticket-list-head,.ticket-list-meta,.ticket-title-line { display: flex; align-items: center; gap: 8px; }.ticket-list-head { justify-content: space-between; }.ticket-list code,.ticket-detail-header code { color: var(--primary); font-size: 10px; font-weight: 700; }.ticket-list > button > strong { overflow: hidden; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }.ticket-list > button > p { margin: -3px 0 0; overflow: hidden; color: var(--muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.ticket-list-meta { color: var(--muted); font-size: 9px; }.ticket-list-meta span:last-child { margin-left: auto; }.urgent,.priority-badge { color: var(--danger); font-weight: 700; }.ticket-state { padding: 50px 20px; color: var(--muted); text-align: center; }
.ticket-detail-panel { display: flex; flex-direction: column; }.ticket-detail-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; padding: 20px; border-bottom: 1px solid var(--line); }.ticket-detail-header h2 { margin: 10px 0 5px; font-size: 19px; }.ticket-detail-header p { margin: 0; color: var(--muted); font-size: 10px; }.ticket-detail-actions { flex: 0 0 auto; }.status-select { display: grid; gap: 4px; }.status-select span { color: var(--muted); font-size: 9px; }.status-select select { min-width: 150px; min-height: 34px; padding-block: 5px; font-size: 11px; }.priority-badge { padding: 3px 7px; border-radius: 999px; background: var(--danger-soft); font-size: 9px; }
.ticket-timeline { flex: 1; min-height: 340px; max-height: 520px; display: grid; align-content: start; gap: 14px; overflow-y: auto; padding: 22px; background: #fbfcfe; }.ticket-message { max-width: 78%; display: grid; gap: 10px; justify-self: start; padding: 13px 15px; border: 1px solid var(--line); border-radius: 4px 13px 13px 13px; background: #fff; }.ticket-message.admin { justify-self: end; border-color: #bfdbfe; border-radius: 13px 4px 13px 13px; background: #eff6ff; }.ticket-message header { display: flex; align-items: center; gap: 8px; }.message-avatar { width: 25px; height: 25px; display: grid; place-items: center; border-radius: 50%; color: #fff; background: #64748b; font-size: 9px; font-weight: 750; }.ticket-message.admin .message-avatar { background: var(--primary); }.ticket-message header div { display: grid; gap: 1px; }.ticket-message strong { font-size: 10px; }.ticket-message time { color: var(--muted); font-size: 8px; }.ticket-message > p { margin: 0; color: #344054; font-size: 12px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }.timeline-event { display: flex; align-items: center; justify-content: center; gap: 7px; }.timeline-event > span { width: 6px; height: 6px; border-radius: 50%; background: #94a3b8; }.timeline-event p { margin: 0; color: var(--muted); font-size: 9px; }
.ticket-reply { display: grid; gap: 10px; padding: 16px 20px; border-top: 1px solid var(--line); }.ticket-reply textarea { min-height: 90px; }.ticket-reply footer { display: flex; align-items: center; justify-content: space-between; }.ticket-reply small { color: var(--muted); font-size: 9px; }.closed-notice { display: flex; align-items: center; gap: 11px; padding: 18px 20px; border-top: 1px solid var(--line); background: var(--success-soft); }.closed-notice > .ui-icon { color: var(--success); font-size: 20px; }.closed-notice strong { font-size: 12px; }.closed-notice p { margin: 2px 0 0; color: var(--muted); font-size: 10px; }.ticket-detail-empty { min-height: 600px; display: grid; place-items: center; align-content: center; text-align: center; }.ticket-detail-empty > span { width: 52px; height: 52px; display: grid; place-items: center; border-radius: 14px; color: var(--primary); background: var(--primary-soft); font-size: 23px; }.ticket-detail-empty h2 { margin: 14px 0 5px; font-size: 17px; }.ticket-detail-empty p { color: var(--muted); font-size: 11px; }
@media (max-width: 1000px) { .ticket-workspace { grid-template-columns: 1fr; }.ticket-list { max-height: 360px; }.ticket-detail-empty { min-height: 320px; } }
@media (max-width: 680px) { .ticket-summary { grid-template-columns: 1fr; }.ticket-workspace { min-height: 0; }.ticket-detail-header { display: grid; }.ticket-message { max-width: 92%; }.ticket-toolbar .toolbar-group,.ticket-toolbar .search-field,.ticket-toolbar select { width: 100%; } }
</style>
