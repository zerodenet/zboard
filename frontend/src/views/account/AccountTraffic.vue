<template>
  <section class="account-page stack">
    <PageHeader title="流量明细" description="按时间范围查看总量、今日消耗和每条计费记录。" eyebrow="TRAFFIC">
      <template #actions>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="loadAll">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <TransientFeedback :error="error" error-title="流量信息加载失败" />

    <UiMetricStrip :columns="3">
      <MetricCard
        label="剩余可用"
        :value="formatBytes(summary.remaining_bytes)"
        icon="activity"
        status="可用"
        tone="info"
        meta="当前有效订阅汇总"
      />
      <MetricCard
        label="累计使用"
        :value="formatBytes(summary.total_used_bytes)"
        icon="database"
        status="历史"
        tone="info"
        icon-tone="success"
        meta="历史计费流量"
      />
      <MetricCard
        label="今日使用"
        :value="formatBytes(summary.used_bytes_today)"
        icon="clock"
        status="今日"
        tone="info"
        icon-tone="warning"
      >
        <template #meta><TimeBadge :value="summary.as_of" mode="relative" /></template>
      </MetricCard>
    </UiMetricStrip>

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(subscriptionFilter || from || to)" @clear="clearFilters">
          <WorkbenchFilterInput v-model="subscriptionFilter" label="订阅 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterDate v-model:from="from" v-model:to="to" label="记录日期" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions>
        <span class="muted">范围内计费 {{ formatBytes(aggregates.used_bytes) }}</span>
      </template>

      <DataTable v-if="records.length" caption="我的流量使用记录" :row-count="total" :min-width="760">
        <thead>
          <tr>
            <th class="table-primary-column">时间</th>
            <th>订阅</th>
            <th data-column-priority="2">上行</th>
            <th data-column-priority="2">下行</th>
            <th data-column-priority="3">端点倍率</th>
            <th>计费流量</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="record in records" :key="record.id">
            <td class="table-primary-column"><TimeBadge :value="record.record_at" /></td>
            <td class="numeric-column">{{ record.subscription_id || '—' }}</td>
            <td class="numeric-column" data-column-priority="2">{{ formatBytes(record.upload_bytes) }}</td>
            <td class="numeric-column" data-column-priority="2">{{ formatBytes(record.download_bytes) }}</td>
            <td class="numeric-column" data-column-priority="3">{{ formatMultiplier(record.protocol_multiplier_milli) }}</td>
            <td class="numeric-column"><strong>{{ formatBytes(record.used_bytes) }}</strong></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState
        v-else
        icon="activity"
        title="还没有流量记录"
        description="节点开始上报使用量后，明细会显示在这里。"
      />

      <template #footer>
        <CursorPager
          :count="records.length"
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

    <aside class="account-notice">
      <UiIcon name="activity" />
      <div>
        <strong>为什么计费流量可能不同？</strong>
        <p>套餐可以按上行、下行或双向选择原始流量，随后只应用协议端点倍率；这里展示最终实际扣除量。</p>
      </div>
    </aside>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  fetchAccountTrafficRecordsPage,
  fetchTrafficSummary,
  type TrafficRecordAggregates,
  type TrafficRecordSummary,
} from '../../api/client'
import CursorPager from '../../components/CursorPager.vue'
import DataTable from '../../components/DataTable.vue'
import DataWorkbench from '../../components/DataWorkbench.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../../components/WorkbenchFilterDate.vue'
import WorkbenchFilterInput from '../../components/WorkbenchFilterInput.vue'
import { resolveHistoryRange } from '../../composables/historyState'
import { useCursorTable } from '../../composables/useCursorTable'
import { formatBytes } from '../../utils/format'

const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 25)
const cursor = ref(String(route.query.cursor || ''))
const initialRange = resolveHistoryRange(route.query, 7)
const from = ref(initialRange.from)
const to = ref(initialRange.to)
const subscriptionFilter = ref(String(route.query.subscription_id || ''))
const summary = ref<Record<string, any>>({})
const summaryLoading = ref(false)
const summaryError = ref('')

const {
  items: records,
  total,
  aggregates,
  nextCursor,
  previousCursor,
  loading: recordLoading,
  refreshing,
  error: recordError,
  load: loadRecords,
} = useCursorTable<TrafficRecordSummary, TrafficRecordAggregates>({
  fetchPage: ({ signal }) => fetchAccountTrafficRecordsPage({
    subscriptionId: Number(subscriptionFilter.value) || undefined,
    cursor: cursor.value || undefined,
    from: from.value,
    to: to.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '流量记录加载失败。',
})

const loading = computed(() => recordLoading.value || summaryLoading.value)
const error = computed(() => recordError.value || summaryError.value)

function formatMultiplier(value: number) {
  return `${(Number(value || 1000) / 1000).toLocaleString('zh-CN', { maximumFractionDigits: 3 })}×`
}

async function loadSummary() {
  summaryLoading.value = true
  summaryError.value = ''
  try {
    summary.value = await fetchTrafficSummary()
  } catch (cause: any) {
    summaryError.value = cause?.response?.data?.message || '流量汇总加载失败。'
  } finally {
    summaryLoading.value = false
  }
}

async function loadAll() {
  await Promise.all([loadSummary(), loadRecords()])
}

async function syncURL(replace = false) {
  const location = {
    query: {
      ...(subscriptionFilter.value ? { subscription_id: subscriptionFilter.value } : {}),
      from: from.value,
      to: to.value,
      ...(cursor.value ? { cursor: cursor.value } : {}),
      ...(limit.value !== 25 ? { limit: String(limit.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
}

function normalizeRange() {
  const range = resolveHistoryRange({ from: from.value, to: to.value }, 7)
  from.value = range.from
  to.value = range.to
}

async function applyFilters() {
  normalizeRange()
  cursor.value = ''
  await syncURL()
  await loadRecords()
}

async function clearFilters() {
  subscriptionFilter.value = ''
  cursor.value = ''
  await syncURL()
  await loadRecords()
}

async function changeCursor(value: string | null) {
  if (!value) return
  cursor.value = value
  await syncURL()
  await loadRecords()
}

async function changeLimit(value: number) {
  limit.value = allowedPageSizes.includes(value) ? value : 25
  cursor.value = ''
  await syncURL()
  await loadRecords()
}

watch(() => route.fullPath, async () => {
  const nextSubscription = String(route.query.subscription_id || '')
  const nextCursorValue = String(route.query.cursor || '')
  const nextRange = resolveHistoryRange(route.query, 7)
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 25
  if (
    nextSubscription !== subscriptionFilter.value
    || nextCursorValue !== cursor.value
    || nextRange.from !== from.value
    || nextRange.to !== to.value
    || nextLimit !== limit.value
  ) {
    subscriptionFilter.value = nextSubscription
    cursor.value = nextCursorValue
    from.value = nextRange.from
    to.value = nextRange.to
    limit.value = nextLimit
    await loadRecords()
  }
})

onMounted(async () => {
  if (!route.query.from || !route.query.to) await syncURL(true)
  await loadAll()
})
</script>
