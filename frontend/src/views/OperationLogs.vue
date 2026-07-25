<template>
  <section class="standard-page">
    <PageHeader title="运行日志" description="以紧凑摘要查看协议发布、节点内核和后台任务；输出与错误仅在打开详情时加载。" eyebrow="Operations">
      <template #actions><PageRefreshButton label="刷新运行日志" :loading="loading" @click="load" /></template>
    </PageHeader>

    <TransientFeedback :error="error" error-title="运行日志加载失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="hasFilters" :loading="loading" @clear="clearFilters">
          <WorkbenchFilterSelect v-model="source" label="来源" :options="sourceOptions" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="status" label="状态" :options="statusOptions" @apply="applyFilters" />
          <WorkbenchFilterNumber v-model="nodeID" label="节点 ID" value-prefix="#" :min="0" @apply="applyFilters" />
          <WorkbenchFilterNumber v-model="endpointID" label="协议端点 ID" value-prefix="#" :min="0" @apply="applyFilters" />
          <WorkbenchFilterDate v-model:from="from" v-model:to="to" label="记录日期" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable v-if="items.length" caption="运行日志摘要；完整输出和错误在详情抽屉中按需加载" :row-count="total" :min-width="1080" table-class="operation-table">
          <thead><tr><th data-column-priority="2">时间</th><th class="table-primary-column">来源与动作</th><th data-column-priority="2">目标</th><th>状态</th><th data-column-priority="3">摘要</th><th data-column-priority="3">结果内容</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
          <tbody><tr v-for="item in items" :key="`${item.source}:${item.id}`">
            <td data-column-priority="2"><TimeBadge :value="item.created_at" /></td>
            <td class="table-primary-column"><div class="cell-title"><strong><UiIcon :name="sourceIcon(item.source)" />{{ sourceLabel(item.source) }}</strong><span>{{ adminActionLabel(item.action) }} · #{{ item.id }}</span></div></td>
            <td data-column-priority="2">{{ operationTargetLabel(item) }}</td>
            <td><StatusBadge :tone="statusTone(item.status)">{{ statusLabel(item.status) }}</StatusBadge></td>
            <td data-column-priority="3">{{ operationSummaryLabel(item) }}</td>
            <td data-column-priority="3"><StatusBadge v-if="item.has_error" tone="danger" icon="alert">有错误</StatusBadge><StatusBadge v-else-if="item.has_output" tone="info" icon="terminal">有输出</StatusBadge><span v-else>—</span></td>
            <td class="table-action-column"><UiButton variant="secondary" size="sm" type="button" :loading="detailLoadingKey === `${item.source}:${item.id}`" @click="openDetail(item)">查看</UiButton></td>
          </tr></tbody>
      </DataTable>
      <EmptyState v-else icon="terminal" title="没有匹配的运行记录" description="执行协议发布、内核操作或后台任务后，结果会显示在这里。" />
      <template #footer><CursorPager :count="items.length" :total="total" :limit="limit" :loading="loading" :has-previous="Boolean(previousCursor)" :has-next="Boolean(nextCursor)" @previous="changeCursor(previousCursor)" @next="changeCursor(nextCursor)" @limit="changeLimit" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(selectedItem)" :title="selectedItem ? `${sourceLabel(selectedItem.source)} #${selectedItem.id}` : '运行详情'" :description="selectedItem ? adminActionLabel(selectedItem.action) : '按需加载的持久化运行结果'" @close="closeDetail">
      <div v-if="selectedItem" class="stack">
        <PageAlert v-if="detailError" tone="danger" title="运行详情加载失败">{{ detailError }}</PageAlert>
        <dl class="detail-kv"><div><dt>状态</dt><dd><StatusBadge :tone="statusTone(selectedItem.status)">{{ statusLabel(selectedItem.status) }}</StatusBadge></dd></div><div><dt>目标</dt><dd>{{ operationTargetLabel(selectedItem) }}</dd></div><div><dt>开始时间</dt><dd><TimeBadge :value="selectedItem.started_at || selectedItem.created_at" /></dd></div><div><dt>结束时间</dt><dd><TimeBadge :value="selectedItem.finished_at" /></dd></div></dl>
        <p v-if="selectedItem.summary" class="operation-summary">{{ operationSummaryLabel(selectedItem) }}</p>
        <OutputBlock v-if="selectedItem.error" :value="selectedItem.error" label="错误" tone="danger" />
        <OutputBlock v-if="selectedItem.output" :value="selectedItem.output" label="输出" />
        <EmptyState v-if="!selectedItem.error && !selectedItem.output" icon="terminal" title="没有输出正文" description="该运行记录只保存了状态和摘要。" />
      </div>
    </DetailDrawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchOperationLog, fetchOperationLogs, type OperationLog } from '../api/client'
import CursorPager from '../components/CursorPager.vue'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import EmptyState from '../components/EmptyState.vue'
import OutputBlock from '../components/OutputBlock.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../components/WorkbenchFilterDate.vue'
import WorkbenchFilterNumber from '../components/WorkbenchFilterNumber.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { resolveHistoryRange } from '../composables/historyState'
import { useCursorTable } from '../composables/useCursorTable'
import { adminActionLabel, operationSummaryLabel, operationTargetLabel } from '../utils/adminDisplay'
import { formatUnknownValue } from '../utils/format'
import { preserveAdminReturnTo } from '../utils/navigation'

const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const cursor = ref(String(route.query.cursor || ''))
const initialRange = resolveHistoryRange(route.query, 30)
const from = ref(initialRange.from)
const to = ref(initialRange.to)
const source = ref(String(route.query.source || '')), status = ref(String(route.query.status || ''))
const nodeID = ref(Number(route.query.node_id || 0)), endpointID = ref(Number(route.query.protocol_endpoint_id || 0))
const selectedItem = ref<OperationLog | null>(null)
const detailLoadingKey = ref('')
const detailError = ref('')
let detailController: AbortController | null = null
const sourceOptions = [{ label: '全部来源', value: '' }, { label: '协议发布', value: 'protocol_publish' }, { label: '节点内核', value: 'node_kernel' }, { label: '后台任务', value: 'task' }]
const statusOptions = [{ label: '全部状态', value: '' }, { label: '等待中', value: 'queued' }, { label: '执行中', value: 'running' }, { label: '成功', value: 'succeeded' }, { label: '失败', value: 'failed' }]
const hasFilters = computed(() => Boolean(source.value || status.value || nodeID.value || endpointID.value))
const { items, total, nextCursor, previousCursor, loading, refreshing, error, load } = useCursorTable<OperationLog>({
  fetchPage: ({ signal }) => fetchOperationLogs({ source: source.value || undefined, status: status.value || undefined, nodeId: nodeID.value || undefined, protocolEndpointId: endpointID.value || undefined, cursor: cursor.value || undefined, from: from.value, to: to.value, limit: limit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '运行日志加载失败。',
})

function sourceLabel(value: string) { return ({ protocol_publish: '协议发布', node_kernel: '节点内核', task: '后台任务' } as Record<string,string>)[value] || formatUnknownValue('来源', value) }
function sourceIcon(value: string) { return value === 'task' ? 'tasks' : value === 'node_kernel' ? 'nodes' : 'activity' }
function statusLabel(value: string) { return ({ queued: '等待中', running: '执行中', succeeded: '成功', failed: '失败' } as Record<string,string>)[value] || formatUnknownValue('状态', value) }
function statusTone(value: string): 'neutral' | 'info' | 'success' | 'danger' { return value === 'succeeded' ? 'success' : value === 'failed' ? 'danger' : value === 'running' ? 'info' : 'neutral' }
function selectedLogKey() { return selectedItem.value ? `${selectedItem.value.source}:${selectedItem.value.id}` : '' }
async function syncURL(replace = false) { const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(source.value ? { source: source.value } : {}), ...(status.value ? { status: status.value } : {}), ...(nodeID.value ? { node_id: String(nodeID.value) } : {}), ...(endpointID.value ? { protocol_endpoint_id: String(endpointID.value) } : {}), from: from.value, to: to.value, ...(cursor.value ? { cursor: cursor.value } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}), ...(selectedLogKey() ? { log: selectedLogKey() } : {}) } }; await (replace ? router.replace(location) : router.push(location)) }
function normalizeRange() { const range = resolveHistoryRange({ from: from.value, to: to.value }, 30); from.value = range.from; to.value = range.to }
async function applyFilters() { normalizeRange(); cursor.value = ''; await syncURL(); await load() }
async function clearFilters() { source.value = ''; status.value = ''; nodeID.value = 0; endpointID.value = 0; cursor.value = ''; await syncURL(); await load() }
async function changeCursor(value: string | null) { if (!value) return; cursor.value = value; await syncURL(); await load() }
async function changeLimit(value: number) { limit.value = allowedPageSizes.includes(value) ? value : 50; cursor.value = ''; await syncURL(); await load() }
async function loadDetail(sourceValue: OperationLog['source'], id: number, summary?: OperationLog) {
  detailController?.abort()
  detailController = new AbortController()
  const current = detailController
  const key = `${sourceValue}:${id}`
  if (summary) selectedItem.value = summary
  detailLoadingKey.value = key; detailError.value = ''
  try {
    const detail = await fetchOperationLog(sourceValue, id, { signal: current.signal })
    if (!current.signal.aborted && detailLoadingKey.value === key) selectedItem.value = detail
  } catch (cause: any) {
    if (!current.signal.aborted && detailLoadingKey.value === key) detailError.value = cause?.response?.data?.message || '运行详情加载失败。'
  } finally {
    if (detailLoadingKey.value === key) detailLoadingKey.value = ''
  }
}
async function openDetail(item: OperationLog) { selectedItem.value = item; await syncURL(); await loadDetail(item.source, item.id, item) }
async function closeDetail() { detailController?.abort(); detailController = null; selectedItem.value = null; detailError.value = ''; await syncURL() }
function parseLogKey(value: unknown): { source: OperationLog['source']; id: number } | null {
  if (typeof value !== 'string') return null
  const match = /^(protocol_publish|node_kernel|task):(\d+)$/.exec(value)
  if (!match || Number(match[2]) <= 0) return null
  return { source: match[1] as OperationLog['source'], id: Number(match[2]) }
}
watch(() => route.fullPath, async () => {
  const nextSource = String(route.query.source || ''), nextStatus = String(route.query.status || ''), nextNode = Number(route.query.node_id || 0), nextEndpoint = Number(route.query.protocol_endpoint_id || 0)
  const rawLimit = Number(route.query.limit), nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50, nextCursorValue = String(route.query.cursor || ''), nextRange = resolveHistoryRange(route.query, 30)
  if (nextSource !== source.value || nextStatus !== status.value || nextNode !== nodeID.value || nextEndpoint !== endpointID.value || nextLimit !== limit.value || nextCursorValue !== cursor.value || nextRange.from !== from.value || nextRange.to !== to.value) { source.value = nextSource; status.value = nextStatus; nodeID.value = nextNode; endpointID.value = nextEndpoint; limit.value = nextLimit; cursor.value = nextCursorValue; from.value = nextRange.from; to.value = nextRange.to; await load() }
  const nextLog = parseLogKey(route.query.log)
  if (!nextLog && selectedItem.value) { detailController?.abort(); selectedItem.value = null; detailError.value = '' }
  else if (nextLog && selectedLogKey() !== `${nextLog.source}:${nextLog.id}`) await loadDetail(nextLog.source, nextLog.id)
})
onMounted(async () => { if (!route.query.from || !route.query.to || route.query.page) await syncURL(true); await load(); const initialLog = parseLogKey(route.query.log); if (initialLog) await loadDetail(initialLog.source, initialLog.id) })
onBeforeUnmount(() => detailController?.abort())
</script>

<style scoped>
:deep(.operation-table .cell-title strong){display:flex;align-items:center;gap:6px}:deep(.operation-table td:nth-child(5)){max-width:300px;color:var(--muted);white-space:normal}.detail-kv{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));margin:0;border:1px solid var(--line);border-radius:9px}.detail-kv>div{padding:11px}.detail-kv dt{color:var(--muted);font-size:9px}.detail-kv dd{margin:4px 0 0;font-size:11px;font-weight:650}.operation-summary{margin:0;padding:12px;border:1px solid var(--line);border-radius:9px;background:var(--surface-soft);font-size:11px;line-height:1.55}@media(max-width:620px){.detail-kv{grid-template-columns:1fr}}
</style>
