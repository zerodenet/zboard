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

    <TransientFeedback :success="message" :error="error" success-title="实验评估策略已更新" error-title="Fair Use 观测加载失败" />
    <PageAlert tone="info" title="当前阶段仅收集和分析数据">
      原始行为事实最多保留 15 天。页面查询只做实时聚合，不生成新的分析记录；风险评分属于实验参考，不代表用户违规，也不会改变订阅服务状态。
    </PageAlert>

    <UiSection class="observation-selector" title="观测对象" description="按用户或套餐找到订阅后开始观测；订阅 ID 只作为内部定位信息，不需要手工记忆。">
      <WorkbenchFilterBar :active="Boolean(searchQuery) || range !== '1d' || Boolean(subscriptionID)" @clear="clearSelection">
        <WorkbenchFilterInput
          v-model="searchQuery"
          label="查找订阅"
          maxlength="128"
          placeholder="用户邮箱、订阅 ID、套餐或 SKU"
          @apply="searchSubscriptions"
        />
        <WorkbenchFilterSelect v-model="range" label="观测范围" :options="rangeOptions" @apply="applyRange" />
      </WorkbenchFilterBar>

      <PageAlert v-if="pickerError" tone="warning" title="订阅列表加载失败">{{ pickerError }}</PageAlert>

      <div v-if="subscriptionID" class="selected-observation">
        <div class="selected-observation-main">
          <span class="selected-observation-label">当前观测</span>
          <strong>{{ selectedSubscription?.user_email || `订阅 #${subscriptionID}` }}</strong>
          <small v-if="selectedSubscription">
            {{ selectedSubscription.plan_name || `套餐 #${selectedSubscription.plan_id}` }}
            · {{ selectedSubscription.sku_name || `SKU #${selectedSubscription.plan_sku_id}` }}
            · 订阅 #{{ selectedSubscription.id }}
          </small>
          <small v-else>正在读取订阅业务信息…</small>
        </div>
        <StatusBadge v-if="selectedSubscription" :tone="subscriptionStatusTone(selectedSubscription.status)">
          {{ subscriptionStatusLabel(selectedSubscription.status) }}
        </StatusBadge>
        <UiButton variant="secondary" size="sm" type="button" @click="changeSelection">更换订阅</UiButton>
      </div>

      <div v-else class="subscription-picker">
        <div class="subscription-picker-heading">
          <div>
            <strong>{{ searchQuery.trim() ? '匹配订阅' : '最近有效订阅' }}</strong>
            <span>{{ searchQuery.trim() ? `共找到 ${pickerTotal} 条` : '可直接选择，无需输入订阅 ID' }}</span>
          </div>
          <span v-if="pickerLoading" class="section-note">正在查询…</span>
        </div>

        <DataTable v-if="pickerItems.length" caption="Fair Use 可选订阅" :row-count="pickerItems.length" :min-width="820">
          <thead>
            <tr>
              <th class="table-primary-column">用户</th>
              <th>订阅</th>
              <th data-column-priority="2">套餐规格</th>
              <th>状态</th>
              <th data-column-priority="2">到期时间</th>
              <th class="table-action-column"><span class="sr-only">操作</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="sub in pickerItems" :key="sub.id">
              <td class="table-primary-column">
                <div class="cell-title"><strong>{{ sub.user_email || `用户 #${sub.user_id}` }}</strong><span class="mono">#{{ sub.user_id }}</span></div>
              </td>
              <td><strong class="mono">#{{ sub.id }}</strong></td>
              <td data-column-priority="2">
                <div class="cell-title"><strong>{{ sub.plan_name || `套餐 #${sub.plan_id}` }}</strong><span>{{ sub.sku_name || `SKU #${sub.plan_sku_id}` }}</span></div>
              </td>
              <td><StatusBadge :tone="subscriptionStatusTone(sub.status)">{{ subscriptionStatusLabel(sub.status) }}</StatusBadge></td>
              <td data-column-priority="2"><TimeBadge :value="sub.end_at" /></td>
              <td class="table-action-column"><UiButton variant="secondary" size="sm" type="button" @click="selectSubscription(sub)">观测</UiButton></td>
            </tr>
          </tbody>
        </DataTable>
        <EmptyState
          v-else-if="!pickerLoading"
          icon="activity"
          :title="searchQuery.trim() ? '没有匹配订阅' : '当前没有有效订阅'"
          :description="searchQuery.trim() ? '可以按邮箱、订阅 ID、套餐名称或 SKU 继续搜索。' : '有有效订阅后会在这里直接显示可观测对象。'"
        />
      </div>
    </UiSection>

    <template v-if="subscriptionID && observation && metrics && state && policy">
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
            <div><span>时间口径</span><strong>ZBoard 接收时间</strong></div>
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
          <div class="evaluation-policy-action">
            <PageAlert :tone="policy.effective.enabled ? 'info' : 'warning'" :title="policy.effective.enabled ? '当前订阅正在进行只读实验评估' : '实验评估需要先定义参数'">
              策略按“订阅例外 → 套餐 → 平台默认”继承。为当前订阅配置连接频率、工作节点和评分参数后，才会保存一份仅观测策略。
            </PageAlert>
            <div class="evaluation-parameter-summary">
              <div><span>连接启动</span><strong>{{ policy.effective.connection_start_window_seconds }} 秒内 &gt; {{ policy.effective.connection_start_threshold }}</strong></div>
              <div><span>工作节点</span><strong>{{ policy.effective.working_node_window_seconds }} 秒内 &gt; {{ policy.effective.working_node_threshold }}</strong></div>
              <div><span>评估周期</span><strong>每 {{ policy.effective.evaluation_interval_seconds }} 秒</strong></div>
            </div>
            <div class="evaluation-policy-buttons">
              <UiButton type="button" size="sm" :variant="policy.effective.enabled ? 'secondary' : 'primary'" :disabled="loading" @click="openPolicyEditor">{{ policy.effective.enabled ? '调整评估参数' : '配置并启用评估' }}</UiButton>
              <UiButton v-if="policy.effective.enabled" variant="danger" size="sm" type="button" :loading="policySaving" :disabled="loading" @click="setEvaluationEnabled(false)">停用当前订阅评估</UiButton>
            </div>
          </div>
        </UiSection>
      </div>

      <ModalDialog :open="policyEditorOpen" title="配置当前订阅的实验评估" description="保存后创建订阅级策略覆盖；仅生成实验评分和解释事件，不执行通知、限速或暂停。" size="lg" :busy="policySaving" :dirty="policyDraftDirty" @close="policyEditorOpen = false">
        <div ref="policyFormElement" class="stack policy-editor">
          <PageAlert tone="info" title="先定义要评估的行为">连接启动用于观察短时间重连或并发建立行为；工作节点用于观察同一订阅在多个节点同时活动。阈值应结合上方真实分布，而不是直接照搬默认值。</PageAlert>
          <section class="policy-editor-group"><header><strong>连接启动信号</strong><small>窗口内连接启动次数超过阈值时增加评分。</small></header><div class="form-grid"><FormField v-slot="{ controlAttrs }" label="统计窗口" name="fair-use-connection-window" hint="10–3600 秒" :error="policyErrors.fields.connection_start_window_seconds"><UiNumberInput v-model="policyDraft.connection_start_window_seconds" v-bind="controlAttrs" :min="10" :max="3600" suffix=" 秒" /></FormField><FormField v-slot="{ controlAttrs }" label="触发阈值" name="fair-use-connection-threshold" hint="窗口内连接启动次数" :error="policyErrors.fields.connection_start_threshold"><UiNumberInput v-model="policyDraft.connection_start_threshold" v-bind="controlAttrs" :min="1" :max="1000000" /></FormField><FormField v-slot="{ controlAttrs }" label="评分增量" name="fair-use-connection-penalty" :error="policyErrors.fields.connection_start_penalty"><UiNumberInput v-model="policyDraft.connection_start_penalty" v-bind="controlAttrs" :min="1" :max="policyDraft.score_max" suffix=" 分" /></FormField></div></section>
          <section class="policy-editor-group"><header><strong>跨节点信号</strong><small>窗口内工作节点数超过阈值时增加评分。</small></header><div class="form-grid"><FormField v-slot="{ controlAttrs }" label="统计窗口" name="fair-use-node-window" hint="30–3600 秒" :error="policyErrors.fields.working_node_window_seconds"><UiNumberInput v-model="policyDraft.working_node_window_seconds" v-bind="controlAttrs" :min="30" :max="3600" suffix=" 秒" /></FormField><FormField v-slot="{ controlAttrs }" label="节点阈值" name="fair-use-node-threshold" :error="policyErrors.fields.working_node_threshold"><UiNumberInput v-model="policyDraft.working_node_threshold" v-bind="controlAttrs" :min="1" :max="10000" suffix=" 个" /></FormField><FormField v-slot="{ controlAttrs }" label="评分增量" name="fair-use-node-penalty" :error="policyErrors.fields.working_node_penalty"><UiNumberInput v-model="policyDraft.working_node_penalty" v-bind="controlAttrs" :min="1" :max="policyDraft.score_max" suffix=" 分" /></FormField></div></section>
          <section class="policy-editor-group"><header><strong>评分节奏</strong><small>决定多久评估一次、无异常时如何恢复，以及两个观察状态的分界。</small></header><div class="form-grid"><FormField v-slot="{ controlAttrs }" label="评估周期" name="fair-use-interval" hint="30–3600 秒" :error="policyErrors.fields.evaluation_interval_seconds"><UiNumberInput v-model="policyDraft.evaluation_interval_seconds" v-bind="controlAttrs" :min="30" :max="3600" suffix=" 秒" /></FormField><FormField v-slot="{ controlAttrs }" label="评分上限" name="fair-use-score-max" :error="policyErrors.fields.score_max"><UiNumberInput v-model="policyDraft.score_max" v-bind="controlAttrs" :min="10" :max="10000" suffix=" 分" /></FormField><FormField v-slot="{ controlAttrs }" label="每周期恢复" name="fair-use-recovery" :error="policyErrors.fields.recovery_per_interval"><UiNumberInput v-model="policyDraft.recovery_per_interval" v-bind="controlAttrs" :min="1" :max="policyDraft.score_max" suffix=" 分" /></FormField><FormField v-slot="{ controlAttrs }" label="关注阈值" name="fair-use-warning-score" :error="policyErrors.fields.warning_score"><UiNumberInput v-model="policyDraft.warning_score" v-bind="controlAttrs" :min="1" :max="policyDraft.score_max" suffix=" 分" /></FormField><FormField v-slot="{ controlAttrs }" label="高风险阈值" name="fair-use-violation-score" :error="policyErrors.fields.violation_score"><UiNumberInput v-model="policyDraft.violation_score" v-bind="controlAttrs" :min="1" :max="policyDraft.score_max" suffix=" 分" /></FormField></div></section>
          <PageAlert v-if="policyErrors.formError.value" tone="danger" title="评估参数未保存">{{ policyErrors.formError.value }}</PageAlert>
        </div>
        <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="policySaving" @click="requestClose">取消</UiButton><UiButton type="button" :loading="policySaving" @click="savePolicyParameters">{{ policy?.effective.enabled ? '保存参数' : '保存并启用评估' }}</UiButton></template>
      </ModalDialog>

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
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchAdminSubscriptionDetail, fetchSubscriptionsPage, type AdminSubscriptionListItem } from '../api/client'
import {
  fetchFairUseEvents,
  fetchFairUseMetrics,
  fetchFairUseObservations,
  fetchFairUsePolicy,
  fetchFairUseState,
  evaluateSubscriptionFairUse,
  updateSubscriptionFairUsePolicy,
  type FairUseEvent,
  type FairUseMetrics,
  type FairUseObservationRange,
  type FairUseObservationSeries,
  type FairUsePolicy,
  type FairUsePolicyResolution,
  type FairUseState,
} from '../api/fairUse'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import MetricCard from '../components/MetricCard.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiMetricStrip from '../components/UiMetricStrip.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import UiSection from '../components/UiSection.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useFormErrors } from '../composables/useFormState'
import { formatNumber } from '../utils/format'
import { collectFieldErrors, isIntegerInRange } from '../utils/validation'

const route = useRoute()
const router = useRouter()
const rangeOptions = [
  { label: '最近 1 天', value: '1d' },
  { label: '最近 3 天', value: '3d' },
  { label: '最近 7 天', value: '7d' },
  { label: '最近 15 天', value: '15d' },
]
const allowedRanges = new Set(rangeOptions.map(item => item.value))
const searchQuery = ref('')
const range = ref<FairUseObservationRange>(allowedRanges.has(String(route.query.range)) ? String(route.query.range) as FairUseObservationRange : '1d')
const subscriptionID = ref(validSubscriptionID(String(route.query.subscription || '')))
const selectedSubscription = ref<AdminSubscriptionListItem | null>(null)
const pickerItems = ref<AdminSubscriptionListItem[]>([])
const pickerTotal = ref(0)
const pickerLoading = ref(false)
const pickerError = ref('')
const metrics = ref<FairUseMetrics | null>(null)
const observation = ref<FairUseObservationSeries | null>(null)
const state = ref<FairUseState | null>(null)
const policy = ref<FairUsePolicyResolution | null>(null)
const events = ref<FairUseEvent[]>([])
const loading = ref(false)
const policySaving = ref(false)
const policyEditorOpen = ref(false)
const policyFormElement = ref<HTMLElement | null>(null)
const policyErrors = useFormErrors()
const policyDraft = reactive({ evaluation_interval_seconds: 60, connection_start_threshold: 120, connection_start_window_seconds: 60, connection_start_penalty: 10, working_node_threshold: 3, working_node_window_seconds: 300, working_node_penalty: 15, score_max: 100, recovery_per_interval: 8, warning_score: 30, violation_score: 60 })
const policyDraftBaseline = ref('')
const policyDraftDirty = computed(() => policyDraftBaseline.value !== JSON.stringify(policyDraft))
const error = ref('')
const message = ref('')
let controller: AbortController | null = null
let pickerController: AbortController | null = null
let selectedController: AbortController | null = null

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

function subscriptionStatusLabel(value: string) {
  return ({ active: '有效', expired: '已失效', exhausted: '流量耗尽', canceled: '已取消', inactive: '无效' } as Record<string, string>)[value] || value || '未知'
}

function subscriptionStatusTone(value: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (value === 'active') return 'success'
  if (value === 'expired') return 'warning'
  if (value === 'exhausted') return 'danger'
  return 'neutral'
}

async function syncURL() {
  const query: Record<string, string> = {}
  if (subscriptionID.value) query.subscription = String(subscriptionID.value)
  if (range.value !== '1d') query.range = range.value
  await router.push({ query })
}

async function searchSubscriptions() {
  pickerController?.abort()
  pickerController = new AbortController()
  pickerLoading.value = true
  pickerError.value = ''
  try {
    const q = searchQuery.value.trim()
    const page = await fetchSubscriptionsPage({
      q: q || undefined,
      status: q ? undefined : 'active',
      offset: 0,
      limit: 10,
    }, { signal: pickerController.signal })
    pickerItems.value = page.items
    pickerTotal.value = page.total
  } catch (cause: any) {
    if (cause?.name !== 'CanceledError' && cause?.name !== 'AbortError') {
      pickerItems.value = []
      pickerTotal.value = 0
      pickerError.value = cause?.response?.data?.message || '订阅列表查询失败。'
    }
  } finally {
    pickerLoading.value = false
  }
}

async function resolveSelectedSubscription(id: number) {
  selectedController?.abort()
  selectedController = new AbortController()
  selectedSubscription.value = null
  try {
    selectedSubscription.value = await fetchAdminSubscriptionDetail(id, { signal: selectedController.signal })
  } catch (cause: any) {
    if (cause?.name !== 'CanceledError' && cause?.name !== 'AbortError') {
      error.value = cause?.response?.data?.message || `订阅 #${id} 的业务信息加载失败。`
    }
  }
}

async function selectSubscription(sub: AdminSubscriptionListItem) {
  selectedSubscription.value = sub
  subscriptionID.value = sub.id
  error.value = ''
  clearData()
  await syncURL()
  await load()
}

async function changeSelection() {
  subscriptionID.value = 0
  selectedSubscription.value = null
  error.value = ''
  clearData()
  await syncURL()
  await searchSubscriptions()
}

async function applyRange() {
  await syncURL()
}

async function clearSelection() {
  searchQuery.value = ''
  subscriptionID.value = 0
  selectedSubscription.value = null
  range.value = '1d'
  error.value = ''
  pickerError.value = ''
  clearData()
  await router.push({ query: {} })
  await searchSubscriptions()
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
      const status = Number(cause?.status || cause?.response?.status || 0)
      error.value = status === 404 ? `订阅 #${subscriptionID.value} 不存在。` : cause?.message || cause?.response?.data?.message || 'Fair Use 观测加载失败。'
      clearData()
    }
  } finally {
    loading.value = false
  }
}

async function setEvaluationEnabled(enabled: boolean) {
  if (!subscriptionID.value || !policy.value) return
  policySaving.value = true
  error.value = ''
  message.value = ''
  try {
    policy.value = await updateSubscriptionFairUsePolicy(subscriptionID.value, policy.value.effective, enabled)
    if (enabled) await evaluateSubscriptionFairUse(subscriptionID.value)
    message.value = enabled
      ? `订阅 #${subscriptionID.value} 已启用只读实验评估。`
      : `订阅 #${subscriptionID.value} 已停用实验评估。`
    await load()
  } catch (cause: any) {
    error.value = cause?.message || cause?.response?.data?.message || '实验评估策略更新失败。'
  } finally {
    policySaving.value = false
  }
}

function openPolicyEditor() {
  if (!policy.value) return
  const effective = policy.value.effective
  Object.assign(policyDraft, {
    evaluation_interval_seconds: effective.evaluation_interval_seconds,
    connection_start_threshold: effective.connection_start_threshold,
    connection_start_window_seconds: effective.connection_start_window_seconds,
    connection_start_penalty: effective.connection_start_penalty,
    working_node_threshold: effective.working_node_threshold,
    working_node_window_seconds: effective.working_node_window_seconds,
    working_node_penalty: effective.working_node_penalty,
    score_max: effective.score_max,
    recovery_per_interval: effective.recovery_per_interval,
    warning_score: effective.warning_score,
    violation_score: effective.violation_score,
  })
  policyErrors.clear()
  policyDraftBaseline.value = JSON.stringify(policyDraft)
  policyEditorOpen.value = true
}

async function savePolicyParameters() {
  if (!subscriptionID.value || !policy.value) return
  const scoreMax = Number(policyDraft.score_max)
  const valid = await policyErrors.applyValidation(collectFieldErrors({
    evaluation_interval_seconds: !isIntegerInRange(policyDraft.evaluation_interval_seconds, 30, 3600) && '评估周期必须为 30–3600 秒。',
    connection_start_window_seconds: !isIntegerInRange(policyDraft.connection_start_window_seconds, 10, 3600) && '连接统计窗口必须为 10–3600 秒。',
    connection_start_threshold: !isIntegerInRange(policyDraft.connection_start_threshold, 1, 1000000) && '连接阈值必须为 1–1000000。',
    connection_start_penalty: !isIntegerInRange(policyDraft.connection_start_penalty, 1, scoreMax) && '连接评分必须大于 0 且不超过评分上限。',
    working_node_window_seconds: !isIntegerInRange(policyDraft.working_node_window_seconds, 30, 3600) && '节点统计窗口必须为 30–3600 秒。',
    working_node_threshold: !isIntegerInRange(policyDraft.working_node_threshold, 1, 10000) && '节点阈值必须为 1–10000。',
    working_node_penalty: !isIntegerInRange(policyDraft.working_node_penalty, 1, scoreMax) && '节点评分必须大于 0 且不超过评分上限。',
    score_max: !isIntegerInRange(policyDraft.score_max, 10, 10000) && '评分上限必须为 10–10000。',
    recovery_per_interval: !isIntegerInRange(policyDraft.recovery_per_interval, 1, scoreMax) && '恢复分数必须大于 0 且不超过评分上限。',
    warning_score: (!isIntegerInRange(policyDraft.warning_score, 1, scoreMax) || policyDraft.warning_score >= policyDraft.violation_score) && '关注阈值必须大于 0 且低于高风险阈值。',
    violation_score: !isIntegerInRange(policyDraft.violation_score, 1, scoreMax) && '高风险阈值必须大于 0 且不超过评分上限。',
  }), policyFormElement, '请更正标记字段后再保存实验评估参数。')
  if (!valid) return
  policySaving.value = true; error.value = ''; message.value = ''
  try {
    const nextPolicy = { ...policy.value.effective, ...policyDraft } as FairUsePolicy
    policy.value = await updateSubscriptionFairUsePolicy(subscriptionID.value, nextPolicy, true)
    await evaluateSubscriptionFairUse(subscriptionID.value)
    policyDraftBaseline.value = JSON.stringify(policyDraft)
    policyEditorOpen.value = false
    message.value = `订阅 #${subscriptionID.value} 的实验评估参数已保存并启用。`
    await load()
  } catch (cause: any) {
    error.value = cause?.message || cause?.response?.data?.message || '实验评估参数保存失败。'
  } finally { policySaving.value = false }
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
  const nextRange = allowedRanges.has(String(query.range)) ? String(query.range) as FairUseObservationRange : '1d'
  const nextID = validSubscriptionID(String(query.subscription || ''))
  const idChanged = nextID !== subscriptionID.value
  const rangeChanged = nextRange !== range.value
  subscriptionID.value = nextID
  range.value = nextRange

  if (idChanged) {
    clearData()
    selectedSubscription.value = null
    if (nextID) await Promise.all([resolveSelectedSubscription(nextID), load()])
    else if (!pickerItems.value.length) await searchSubscriptions()
    return
  }
  if (rangeChanged && nextID) await load()
})

onMounted(async () => {
  if (subscriptionID.value) await Promise.all([resolveSelectedSubscription(subscriptionID.value), load()])
  else await searchSubscriptions()
})
</script>

<style scoped>
.observation-selector { margin-bottom: 16px; }
.observation-selector :deep(.page-alert) { margin-top: 12px; }
.selected-observation { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; align-items: center; gap: 12px; margin-top: 14px; padding: 14px 16px; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--surface-soft); }
.selected-observation-main { min-width: 0; display: grid; gap: 4px; }
.selected-observation-label { color: var(--muted); font-size: 9px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
.selected-observation-main strong { min-width: 0; overflow-wrap: anywhere; font-size: 13px; }
.selected-observation-main small { color: var(--muted); font-size: 10px; overflow-wrap: anywhere; }
.subscription-picker { margin-top: 14px; overflow: hidden; border: 1px solid var(--line); border-radius: var(--radius-md); }
.subscription-picker-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; border-bottom: 1px solid var(--line); background: var(--surface-soft); }
.subscription-picker-heading > div { display: grid; gap: 3px; }
.subscription-picker-heading strong { font-size: 11px; }
.subscription-picker-heading span { color: var(--muted); font-size: 9px; }
.subscription-picker :deep(.data-table-shell) { border: 0; border-radius: 0; }
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
.evaluation-policy-action { display: grid; justify-items: start; gap: 10px; margin-top: 12px; }
.evaluation-parameter-summary{width:100%;display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1px;overflow:hidden;border:1px solid var(--line);border-radius:9px;background:var(--line)}.evaluation-parameter-summary>div{display:grid;gap:3px;padding:10px;background:var(--surface)}.evaluation-parameter-summary span{color:var(--muted);font-size:9px}.evaluation-parameter-summary strong{font-size:10px}.evaluation-policy-buttons{display:flex;flex-wrap:wrap;gap:8px}.policy-editor-group{display:grid;gap:12px;padding:14px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}.policy-editor-group header{display:grid;gap:3px}.policy-editor-group header strong{font-size:12px}.policy-editor-group header small{color:var(--muted);font-size:9px}
@media (max-width: 760px) {
  .selected-observation { grid-template-columns: 1fr; align-items: start; }
  .selected-observation .button { justify-self: start; }
  .fact-grid { grid-template-columns: 1fr; }
  .evaluation-parameter-summary { grid-template-columns: 1fr; }
}
</style>
