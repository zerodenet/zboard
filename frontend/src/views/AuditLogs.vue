<template>
  <section class="standard-page">
    <PageHeader
      title="审计日志"
      description="列表只保留事件摘要；经服务端脱敏的详情在抽屉中按需加载。"
      eyebrow="Audit Trail"
    >
      <template #actions>
        <PageRefreshButton label="刷新审计日志" :loading="loading" @click="load" />
      </template>
    </PageHeader>

    <TransientFeedback :error="error" error-title="审计日志加载失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="hasFilters" :loading="loading" @clear="clearFilters">
          <WorkbenchFilterInput v-model="actor" label="执行者" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="action" label="动作" placeholder="例如 order.pay" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="target" label="目标" placeholder="例如 order:12" @apply="applyFilters" />
          <WorkbenchFilterDate v-model:from="from" v-model:to="to" label="记录日期" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable
        v-if="items.length"
        caption="安全与业务审计事件摘要；完整详情按需加载"
        :row-count="total"
        :min-width="860"
        table-class="audit-table"
      >
        <thead>
          <tr>
            <th class="table-primary-column">动作</th>
            <th data-column-priority="2">执行者</th>
            <th data-column-priority="1">目标</th>
            <th>时间</th>
            <th data-column-priority="3">详情</th>
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td class="table-primary-column">
              <div class="cell-title">
                <strong>{{ adminActionLabel(item.action) }}</strong>
                <span class="mono">{{ item.action }} · #{{ item.id }}</span>
              </div>
            </td>
            <td data-column-priority="2">
              <div class="actor-cell">
                <span aria-hidden="true">{{ actorInitial(item.actor) }}</span>
                <strong>{{ adminActorLabel(item.actor) }}</strong>
              </div>
            </td>
            <td data-column-priority="1"><div class="cell-title"><strong>{{ auditTargetLabel(item.target) }}</strong><span class="mono">{{ item.target || '—' }}</span></div></td>
            <td><TimeBadge :value="item.created_at" /></td>
            <td data-column-priority="3">
              <StatusBadge :tone="item.has_detail ? 'info' : 'neutral'" :icon="item.has_detail ? 'info' : 'minus'">
                {{ item.has_detail ? '有详情' : '无详情' }}
              </StatusBadge>
            </td>
            <td class="table-action-column">
              <UiButton
                variant="secondary"
                size="sm"
                type="button"
                :data-audit-detail-trigger="item.id"
                :loading="detailLoading && selectedAuditID === item.id"
                @click="openDetail(item)"
              >
                查看
              </UiButton>
            </td>
          </tr>
        </tbody>
      </DataTable>

      <EmptyState
        v-else
        icon="audit"
        title="没有匹配事件"
        description="当前筛选条件下没有审计记录。"
      />

      <template #footer>
        <CursorPager
          :count="items.length"
          :total="total"
          :limit="limit"
          :loading="loading"
          :has-previous="Boolean(previousCursor)"
          :has-next="Boolean(nextCursor)"
          @previous="changeCursor(previousCursor)"
          @next="changeCursor(nextCursor)"
          @limit="changeLimit"
        />
      </template>
    </DataWorkbench>

    <DetailDrawer
      :open="Boolean(selectedAuditID)"
      :title="detail ? adminActionLabel(detail.action) : '审计详情'"
      :description="detail ? auditTargetLabel(detail.target) : '经服务端脱敏和长度限制的事件详情'"
      :return-focus-selector="detailReturnFocusSelector"
      @close="closeDetail"
    >
      <div v-if="detailLoading" class="detail-loading" role="status" aria-live="polite">
        <StatusBadge tone="info" icon="refresh">正在加载审计详情</StatusBadge>
      </div>
      <PageAlert v-else-if="detailError" tone="danger" title="审计详情加载失败">
        {{ detailError }}
        <template #actions>
          <UiButton variant="secondary" size="sm" type="button" @click="retryDetail">重试</UiButton>
        </template>
      </PageAlert>
      <template v-else-if="detail">
        <dl class="detail-kv">
          <div>
            <dt>动作</dt>
            <dd class="detail-semantic-value"><StatusBadge tone="info">{{ adminActionLabel(detail.action) }}</StatusBadge><code>{{ detail.action }}</code></dd>
          </div>
          <div>
            <dt>执行者</dt>
            <dd>{{ adminActorLabel(detail.actor) }}</dd>
          </div>
          <div>
            <dt>目标</dt>
            <dd class="detail-semantic-value">{{ auditTargetLabel(detail.target) }}<code>{{ detail.target || '—' }}</code></dd>
          </div>
          <div>
            <dt>时间</dt>
            <dd><TimeBadge :value="detail.created_at" mode="exact" /></dd>
          </div>
          <div>
            <dt>审计 ID</dt>
            <dd>#{{ detail.id }}</dd>
          </div>
          <div>
            <dt>关联用户</dt>
            <dd>{{ detail.user_id ? `#${detail.user_id}` : '系统事件' }}</dd>
          </div>
        </dl>
        <OutputBlock v-if="detail.detail" :value="detail.detail" label="脱敏详情" />
        <EmptyState
          v-else
          icon="audit"
          title="没有详情正文"
          description="该事件只记录了执行者、动作、目标和时间。"
        />
      </template>
    </DetailDrawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchAuditLog, fetchAuditLogs, type AuditLogDetail, type AuditLogSummary } from '../api/client'
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
import UiButton from '../components/UiButton.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../components/WorkbenchFilterDate.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import UiInput from '../components/UiInput.vue'
import TimeBadge from '../components/TimeBadge.vue'
import { resolveHistoryRange } from '../composables/historyState'
import { useCursorTable } from '../composables/useCursorTable'
import { adminActionLabel, adminActorLabel, auditTargetLabel } from '../utils/adminDisplay'
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
const actor = ref(String(route.query.actor || ''))
const action = ref(String(route.query.action || ''))
const target = ref(String(route.query.target || ''))
const selectedAuditID = ref(validAuditID(route.query.audit))
const detail = ref<AuditLogDetail | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const detailReturnFocusSelector = ref('')
let detailController: AbortController | null = null

const hasFilters = computed(() => Boolean(actor.value || action.value || target.value))
const { items, total, nextCursor, previousCursor, loading, refreshing, error, load } = useCursorTable<AuditLogSummary>({
  fetchPage: ({ signal }) => fetchAuditLogs({
    actor: actor.value || undefined,
    action: action.value || undefined,
    target: target.value || undefined,
    cursor: cursor.value || undefined,
    from: from.value,
    to: to.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '审计日志加载失败。',
})

function validAuditID(value: unknown) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 0
}

function actorInitial(value: string) {
  return (value || 'S').slice(0, 1).toUpperCase()
}

async function syncURL(replace = false) {
  const location = {
    query: {
      ...preserveAdminReturnTo(route.query.return_to),
      ...(actor.value ? { actor: actor.value } : {}),
      ...(action.value ? { action: action.value } : {}),
      ...(target.value ? { target: target.value } : {}),
      from: from.value,
      to: to.value,
      ...(cursor.value ? { cursor: cursor.value } : {}),
      ...(limit.value !== 50 ? { limit: String(limit.value) } : {}),
      ...(selectedAuditID.value ? { audit: String(selectedAuditID.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
}

function normalizeRange() {
  const range = resolveHistoryRange({ from: from.value, to: to.value }, 30)
  from.value = range.from
  to.value = range.to
}

function clearDetailState() {
  detailController?.abort()
  detailController = null
  selectedAuditID.value = 0
  detail.value = null
  detailLoading.value = false
  detailError.value = ''
}

async function applyFilters() {
  normalizeRange()
  cursor.value = ''
  clearDetailState()
  await syncURL()
  await load()
}

async function clearFilters() {
  actor.value = ''
  action.value = ''
  target.value = ''
  cursor.value = ''
  clearDetailState()
  await syncURL()
  await load()
}

async function changeCursor(value: string | null) {
  if (!value) return
  cursor.value = value
  clearDetailState()
  await syncURL()
  await load()
}

async function changeLimit(value: number) {
  limit.value = allowedPageSizes.includes(value) ? value : 50
  cursor.value = ''
  clearDetailState()
  await syncURL()
  await load()
}

async function loadDetail(id: number, summary?: AuditLogSummary) {
  detailController?.abort()
  detailController = new AbortController()
  const current = detailController
  selectedAuditID.value = id
  if (summary) detail.value = { ...summary }
  detailLoading.value = true
  detailError.value = ''
  try {
    const result = await fetchAuditLog(id, { signal: current.signal })
    if (!current.signal.aborted && selectedAuditID.value === id) detail.value = result
  } catch (cause: any) {
    if (!current.signal.aborted && selectedAuditID.value === id) {
      detailError.value = cause?.response?.data?.message || '审计详情加载失败。'
    }
  } finally {
    if (selectedAuditID.value === id) detailLoading.value = false
  }
}

async function openDetail(item: AuditLogSummary) {
  detailReturnFocusSelector.value = `button[data-audit-detail-trigger="${item.id}"]`
  selectedAuditID.value = item.id
  detail.value = { ...item }
  await syncURL()
  await loadDetail(item.id, item)
}

async function closeDetail() {
  clearDetailState()
  await syncURL()
}

async function retryDetail() {
  if (selectedAuditID.value) await loadDetail(selectedAuditID.value, detail.value || undefined)
}

watch(() => route.fullPath, async () => {
  const nextActor = String(route.query.actor || '')
  const nextAction = String(route.query.action || '')
  const nextTarget = String(route.query.target || '')
  const nextCursorValue = String(route.query.cursor || '')
  const nextRange = resolveHistoryRange(route.query, 30)
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50
  const listChanged = nextActor !== actor.value
    || nextAction !== action.value
    || nextTarget !== target.value
    || nextLimit !== limit.value
    || nextCursorValue !== cursor.value
    || nextRange.from !== from.value
    || nextRange.to !== to.value
  if (listChanged) {
    actor.value = nextActor
    action.value = nextAction
    target.value = nextTarget
    limit.value = nextLimit
    cursor.value = nextCursorValue
    from.value = nextRange.from
    to.value = nextRange.to
    await load()
  }
  const nextAuditID = validAuditID(route.query.audit)
  if (!nextAuditID && selectedAuditID.value) {
    clearDetailState()
  } else if (nextAuditID && nextAuditID !== selectedAuditID.value) {
    detailReturnFocusSelector.value = `button[data-audit-detail-trigger="${nextAuditID}"]`
    await loadDetail(nextAuditID)
  }
})

onMounted(async () => {
  if (!route.query.from || !route.query.to || route.query.page) await syncURL(true)
  await load()
  const initialAuditID = validAuditID(route.query.audit)
  if (initialAuditID) {
    detailReturnFocusSelector.value = `button[data-audit-detail-trigger="${initialAuditID}"]`
    await loadDetail(initialAuditID)
  }
})

onBeforeUnmount(() => detailController?.abort())
</script>

<style scoped>
.actor-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.actor-cell > span {
  width: 27px;
  height: 27px;
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  border-radius: 50%;
  color: var(--info);
  background: var(--info-soft);
  font-size: 10px;
  font-weight: 750;
}

.actor-cell strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
}

:deep(.audit-table code),
.detail-kv code {
  padding: 3px 6px;
  border-radius: 5px;
  color: var(--code-text);
  background: var(--code-soft);
}

.detail-kv {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 0 0 14px;
  border-block: 1px solid var(--line);
}

.detail-kv > div {
  min-width: 0;
  padding: 11px 4px;
}

.detail-kv > div:nth-child(even) {
  padding-left: 14px;
  border-left: 1px solid var(--line);
}

.detail-kv > div:nth-child(n + 3) {
  border-top: 1px solid var(--line);
}

.detail-kv dt {
  color: var(--muted);
  font-size: 9px;
}

.detail-kv dd {
  min-width: 0;
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  font-size: 11px;
  font-weight: 650;
}

.detail-semantic-value {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

@media (max-width: 480px) {
  :deep(.audit-table) {
    width: 100%;
    min-width: 100% !important;
    table-layout: fixed;
  }

  :deep(.audit-table .table-primary-column) {
    width: 132px;
    min-width: 132px;
    max-width: 132px;
  }

  :deep(.audit-table th:nth-child(4)),
  :deep(.audit-table td:nth-child(4)) {
    width: 104px;
  }

  :deep(.audit-table .table-action-column) {
    width: 68px;
    min-width: 68px;
  }

  :deep(.audit-table .table-action-column .ui-button) {
    min-width: 0;
    padding-inline: 9px;
  }

  .detail-kv {
    grid-template-columns: 1fr;
  }

  .detail-kv > div:nth-child(even) {
    padding-left: 4px;
    border-left: 0;
  }

  .detail-kv > div:nth-child(n + 2) {
    border-top: 1px solid var(--line);
  }
}
</style>
