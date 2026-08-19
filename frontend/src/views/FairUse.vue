<template>
  <section class="standard-page">
    <PageHeader
      title="Fair Use 观测"
      description="基于最近 15 天原始连接行为进行只读观察和分析；当前阶段不通知、不限速、不暂停订阅，也不执行任何处罚。"
      eyebrow="Observation"
    >
      <template #actions>
        <PageRefreshButton label="刷新 Fair Use 观测" :loading="loading" :disabled="!subscriptionID" @click="load" />
      </template>
    </PageHeader>

    <TransientFeedback :error="error" error-title="Fair Use 观测加载失败" />
    <PageAlert tone="info" title="当前阶段仅收集和分析数据">
      原始行为事实最多保留 15 天。页面查询只做实时聚合，不生成新的分析记录；风险评分属于实验参考，不代表用户违规，也不会改变订阅服务状态。
    </PageAlert>

    <UiSection class="observation-selector" title="观测对象" description="选择一个订阅和时间范围。最长只能查询原始数据保留窗口内的 15 天。">
      <WorkbenchFilterBar :active="Boolean(subscriptionInput) || range !== '1d'" @clear="clearSelection">
        <WorkbenchFilterInput
          v-model="subscriptionInput"
          label="订阅 ID"
          value-prefix="#"
          inputmode="numeric"
          placeholder="例如 42"
          @apply="applySelection"
        />
        <WorkbenchFilterSelect v-model="range" label="观测范围" :options="rangeOptions" @apply="applySelection" />
      </WorkbenchFilterBar>
    </UiSection>

    <EmptyState
      v-if="!subscriptionID"
      icon="activity"
      title="选择一个订阅开始观测"
      description="输入订阅 ID 后，可以查看连接启动频率、工作节点分布、coverage 和实验评分。"
    />

    <template v-else-if="observation && metrics && state && policy">
      <UiMetricStrip class="observation-summary">
        <MetricCard
          label="当前活动连接"
          :value="metrics.current_active_flows == null ? '—' : formatNumber(metrics.current_active_flows)"
          icon="activity"
          status="当前快照"
          tone="info"
          :meta="metrics.last_received_at ? '最近仍有行为事件到达' : '暂无最近行为事件'"
        />
        <MetricCard
          :label="`${rangeLabel}连接启动`"
          :value="formatNumber(observation.total_connection_starts)"
          icon="audit"
          status="原始事实"
          tone="info"
          :meta="`${formatNumber(observation.active_buckets)} 个活跃时间桶`"
        />
        <MetricCard
          label="连接启动 P95"
          :value="formatNumber(observation.p95_connection_starts_per_bucket)"
          icon="dashboard"
          status="按时间桶"
          tone="info"
          :meta="`桶宽 ${bucketLabel}`"
        />
        <MetricCard
          label="工作节点 P95"
          :value="formatNumber(observation.p95_working_nodes_per_bucket)"
          icon="nodes"
          status="跨节点行为"
          tone="info"
          :meta="`${formatNumber(observation.distinct_working_nodes)} 个节点在范围内出现过`"
        />
      </UiMetricStrip>

      <div class="section-grid observation-context">
        <UiSection class="span-7" title="数据可信度" description="只有连续 coverage 足够完整时，行为数据才适合用于实验评分和阈值研究。">
          <template #meta>
            <StatusBadge :tone="coverageTone(observation.telemetry_completeness)">{{ coverageLabel(observation.telemetry_completeness) }}</StatusBadge>
          </template>
          <div class="fact-grid">
            <div><span>观测时间</span><strong>{{ rangeLabel }}</strong></div>
            <div><span>数据保留</span><strong>{{ observation.retention_days }} 天</strong></div>
            <div><span>时间口径</span><strong>Zboard 接收时间</strong></div>
            <div><span>节点覆盖</span><strong>{{ observation.coverage.complete_nodes }} / {{ observation.coverage.required_nodes }}</strong></div>
            <div><span>最近行为</span><TimeBadge :value="metrics.last_received_at || null" /></div>
            <div><span>Coverage 原因</span><strong>{{ coverageReason(observation.coverage.reason) }}</strong></div>
          </div>
          <PageAlert v-if="observation.telemetry_completeness !== 'complete'" tone="warning" title="当前范围不能视为完整样本">
            本页仍展示已收到的数据，但缺口、Core 重启或 Connector 中断可能使统计偏低。不要据此定义处罚阈值。
          </PageAlert>
        </UiSection>

        <UiSection class="span-5" title="实验评分" description="评分用于验证模型是否有分析价值，不是违规判定。">
          <template #meta>
            <StatusBadge :tone="riskTone(state.state)">{{ riskLabel(state.state) }}</StatusBadge>
          </template>
          <div class="experiment-score">
            <strong>{{ formatNumber(state.score || 0) }}</strong>
            <span>/ {{ formatNumber(policy.effective.score_max || 100) }}</span>
          </div>
          <div class="fact-grid compact-facts">
            <div><span>策略来源</span><strong>{{ policySourceLabel(policy.source.scope_type, policy.source.scope_id) }}</strong></div>
            <div><span>实验评估</span><strong>{{ policy.effective.enabled ? '已启用' : '未启用' }}</strong></div>
            <div><span>关注阈值</span><strong>{{ policy.effective.warning_score }}</strong></div>
            <div><span>高风险阈值</span><strong>{{ policy.effective.violation_score }}</strong></div>
            <div><span>上次评估</span><TimeBadge :value="state.last_evaluated_at || null" /></div>
            <div><span>评分数据</span><strong>{{ coverageLabel(state.telemetry_completeness) }}</strong></div>
          </div>
        </UiSection>
      </div>

      <UiSection
        title="连接行为分布"
        :description="`${rangeLabel}按 ${bucketLabel} 实时聚合原始 flow.started；结果不会写回数据库。`"
      >
        <template #meta><span class="section-note">P50 {{ observation.p50_connection_starts_per_bucket }} · P95 {{ observation.p95_connection_starts_per_bucket }} · Max {{ observation.max_connection_starts_per_bucket }}</span></template>
        <DataTable v-if="displayBuckets.length" caption="Fair Use 连接行为时间桶" :row-count="displayBuckets.length" :min-width="720">
          <thead>
            <tr>
              <th class="table-primary-column">时间桶</th>
              <th class="numeric-column">连接启动</th>
              <th class="numeric-column">工作节点</th>
              <th data-column-priority="2">活跃状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="bucket in displayBuckets" :key="bucket.start_at">
              <td class="table-primary-column"><TimeBadge :value="bucket.start_at" mode="exact" /></td>
              <td class="numeric-column"><strong>{{ formatNumber(bucket.connection_starts) }}</strong></td>
              <td class="numeric-column">{{ formatNumber(bucket.working_nodes) }}</td>
              <td data-column-priority="2"><StatusBadge :tone="bucket.connection_starts ? 'info' : 'neutral'">{{ bucket.connection_starts ? '有行为' : '空闲' }}</StatusBadge></td>
            </tr>
          </tbody>
        </DataTable>
        <EmptyState v-else icon="activity" title="当前范围没有连接启动记录" description="这不代表异常，只表示当前原始观测窗口没有对应事件。" />
      </UiSection>

      <UiSection class="evaluation-events" title="实验评分变化" description="这里只展示周期评估产生的解释事件；这些派生数据同样最多保留 15 天。">
        <DataTable v-if="events.length" caption="Fair Use 实验评分解释事件" :row-count="events.length" :min-width="820">
          <thead>
            <tr>
              <th class="table-primary-column">时间</th>
              <th>类型</th>
              <th>状态变化</th>
              <th class="numeric-column">分数变化</th>
              <th data-column-priority="2">原因</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="event in events" :key="event.id">
              <td class="table-primary-column"><TimeBadge :value="event.occurred_at" /></td>
              <td><StatusBadge tone="neutral">{{ eventTypeLabel(event.event_type) }}</StatusBadge></td>
              <td>{{ riskLabel(event.state_before) }} → {{ riskLabel(event.state_after) }}</td>
              <td class="numeric-column">{{ signed(event.score_after - event.score_before) }}</td>
              <td data-column-priority="2" class="reason-cell">{{ event.reason || '—' }}</td>
            </tr>
          </tbody>
        </DataTable>
        <EmptyState v-else icon="audit" title="还没有实验评分变化" description="策略未启用、coverage 不完整或行为稳定时都可能没有解释事件。" />
      </UiSection>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  fetchFairUseEvents,
  fetchFairUseMetrics,
  fetchFairUseObservations,
  fetchFairUsePolicy,
  fetchFairUseState,
  type FairUseEvent,
  type FairUseMetrics,
  type FairUseObservationRange,
  type FairUseObservationSeries,
  type FairUsePolicyResolution,
  type FairUseState,
} from '../api/fairUse'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import MetricCard from '../components/MetricCard.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiMetricStrip from '../components/UiMetricStrip.vue'
import UiSection from '../components/UiSection.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { formatNumber } from '../utils/format'

const route = useRoute()
const router = useRouter()
const rangeOptions = [
  { label: '最近 1 天', value: '1d' },
  { label: '最近 3 天', value: '3d' },
  { label: '最近 7 天', value: '7d' },
  { label: '最近 15 天', value: '15d' },
]
const allowedRanges = new Set(rangeOptions.map(item => item.value))
const subscriptionInput = ref(String(route.query.subscription || ''))
const range = ref<FairUseObservationRange>(allowedRanges.has(String(route.query.range)) ? String(route.query.range) as FairUseObservationRange : '1d')
const subscriptionID = ref(validSubscriptionID(subscriptionInput.value))
const metrics = ref<FairUseMetrics | null>(null)
const observation = ref<FairUseObservationSeries | null>(null)
const state = ref<FairUseState | null>(null)
const policy = ref<FairUsePolicyResolution | null>(null)
const events = ref<FairUseEvent[]>([])
const loading = ref(false)
const error = ref('')
let controller: AbortController | null = null

const rangeLabel = computed(() => rangeOptions.find(item => item.value === range.value)?.label || range.value)
const bucketLabel = computed(() => {
  const seconds = observation.value?.bucket_seconds || 0
  if (seconds >= 3600) return `${seconds / 3600} 小时`
  return `${seconds / 60} 分钟`
})
const displayBuckets = computed(() => [...(observation.value?.buckets || [])].reverse())

function validSubscriptionID(raw: string) {
  const value = Number(raw)
  return Number.isInteger(value) && value > 0 ? value : 0
}

async function syncURL() {
  const query: Record<string, string> = {}
  if (subscriptionID.value) query.subscription = String(subscriptionID.value)
  if (range.value !== '1d') query.range = range.value
  await router.push({ query })
}

async function applySelection() {
  subscriptionID.value = validSubscriptionID(subscriptionInput.value)
  if (!subscriptionID.value) {
    error.value = subscriptionInput.value.trim() ? '请输入有效的订阅 ID。' : ''
    clearData()
    await syncURL()
    return
  }
  await syncURL()
  await load()
}

async function clearSelection() {
  subscriptionInput.value = ''
  subscriptionID.value = 0
  range.value = '1d'
  error.value = ''
  clearData()
  await router.push({ query: {} })
}

function clearData() {
  metrics.value = null
  observation.value = null
  state.value = null
  policy.value = null
  events.value = []
}

async function load() {
  if (!subscriptionID.value) return
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  try {
    const signal = controller.signal
    const [nextMetrics, nextObservation, nextState, nextPolicy, nextEvents] = await Promise.all([
      fetchFairUseMetrics(subscriptionID.value, signal),
      fetchFairUseObservations(subscriptionID.value, range.value, signal),
      fetchFairUseState(subscriptionID.value, signal),
      fetchFairUsePolicy(subscriptionID.value, signal),
      fetchFairUseEvents(subscriptionID.value, signal),
    ])
    metrics.value = nextMetrics
    observation.value = nextObservation
    state.value = nextState
    policy.value = nextPolicy
    events.value = nextEvents
  } catch (cause: any) {
    if (cause?.name !== 'AbortError') {
      error.value = cause?.status === 404 ? `订阅 #${subscriptionID.value} 不存在。` : cause?.message || 'Fair Use 观测加载失败。'
      clearData()
    }
  } finally {
    loading.value = false
  }
}

function coverageLabel(value: string) {
  return value === 'complete' ? 'Coverage 完整' : value === 'incomplete' ? 'Coverage 不完整' : 'Coverage 未知'
}
function coverageTone(value: string): 'success' | 'warning' | 'neutral' {
  return value === 'complete' ? 'success' : value === 'incomplete' ? 'warning' : 'neutral'
}
function coverageReason(value: string) {
  return ({
    all_active_credential_nodes_continuous: '所有相关节点连续',
    one_or_more_nodes_incomplete: '至少一个节点存在缺口',
    one_or_more_nodes_unknown: '至少一个节点覆盖未知',
    no_active_credentials: '没有有效凭证节点',
  } as Record<string, string>)[value] || value || '—'
}
function riskLabel(value: string) {
  return value === 'violated' ? '高风险（实验）' : value === 'suspected' ? '需要关注（实验）' : '基线'
}
function riskTone(value: string): 'danger' | 'warning' | 'success' {
  return value === 'violated' ? 'danger' : value === 'suspected' ? 'warning' : 'success'
}
function policySourceLabel(scope: string, id: number) {
  if (scope === 'subscription') return `订阅例外 #${id}`
  if (scope === 'plan') return `套餐 #${id}`
  return '平台默认'
}
function eventTypeLabel(value: string) {
  return ({ coverage_changed: 'Coverage 变化', coverage_restored: 'Coverage 恢复', risk_increased: '风险上升', risk_recovered: '风险恢复', state_changed: '状态变化' } as Record<string, string>)[value] || value
}
function signed(value: number) { return value > 0 ? `+${value}` : String(value) }

watch(() => route.query, async query => {
  const nextInput = String(query.subscription || '')
  const nextRange = allowedRanges.has(String(query.range)) ? String(query.range) as FairUseObservationRange : '1d'
  const nextID = validSubscriptionID(nextInput)
  const changed = nextID !== subscriptionID.value || nextRange !== range.value
  subscriptionInput.value = nextInput
  subscriptionID.value = nextID
  range.value = nextRange
  if (changed) {
    if (nextID) await load()
    else clearData()
  }
})

onMounted(async () => {
  if (subscriptionID.value) await load()
})
</script>

<style scoped>
.observation-selector { margin-bottom: 16px; }
.observation-summary { margin: 16px 0; }
.observation-context { margin-bottom: 16px; }
.fact-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1px; margin: 0 0 14px; overflow: hidden; border: 1px solid var(--line); border-radius: 10px; background: var(--line); }
.fact-grid > div { min-width: 0; display: grid; gap: 4px; padding: 12px; background: var(--surface); }
.fact-grid span { color: var(--muted); font-size: 10px; }
.fact-grid strong { min-width: 0; overflow-wrap: anywhere; font-size: 12px; }
.compact-facts { margin-bottom: 0; }
.experiment-score { display: flex; align-items: baseline; gap: 5px; padding: 18px 20px 4px; }
.experiment-score strong { font-size: 32px; line-height: 1; }
.experiment-score span { color: var(--muted); font-size: 12px; }
.section-note { color: var(--muted); font-size: 11px; }
.reason-cell { max-width: 420px; color: var(--muted); font-size: 11px; overflow-wrap: anywhere; }
.evaluation-events { margin-top: 16px; }
@media (max-width: 760px) { .fact-grid { grid-template-columns: 1fr; } }
</style>
