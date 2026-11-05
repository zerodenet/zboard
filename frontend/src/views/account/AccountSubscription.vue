<template>
  <section class="account-page stack">
    <PageHeader title="订阅配置">
      <template #actions>
        <PageRefreshButton label="刷新订阅" :loading="loading" :disabled="working" @click="loadAll" />
      </template>
    </PageHeader>

    <TransientFeedback
      :success="message"
      :error="error"
      success-title="订阅配置已更新"
      error-title="订阅操作失败"
    />

    <PluginSlot name="account.subscriptions.actions" surface="account" />

    <div ref="accessPanel" class="subscription-access-panel" tabindex="-1">
    <UiSection variant="outlined"
      :title="selectedSubscription?.plan_name || '订阅链接'"
      :description="selectedSubscription?.sku_name || ''"
      class="subscription-access-section"
    >
      <template #actions>
        <UiButton v-if="subscriptionUrl" type="button" @click="copy"><UiIcon name="copy" />{{ copyLabel }}</UiButton>
        <UiButton v-else-if="selectedSubscription && !access.configured" type="button" :disabled="!accessReady || access.delivery_configured === false || !selectedContextReady || working || selectedSubscription.flow_used >= selectedSubscription.flow_total" @click="ask('generate')">生成订阅链接</UiButton>
      </template>
      <div v-if="selectedSubscription" class="panel-body access-panel">
        <PageAlert v-if="selectedSubscriptionResource.stale.value" tone="warning" title="订阅详情尚未更新">
          暂时无法核对当前额度和期限，显示上次读取的信息。请刷新后再生成、轮换或吊销链接。
        </PageAlert>
        <PageAlert v-if="accessResource.error.value" tone="danger" title="订阅链接读取失败">
          {{ accessResource.error.value }}
          <UiButton variant="secondary" size="sm" type="button" @click="loadAccess">重试链接</UiButton>
        </PageAlert>
        <PageAlert v-else-if="selectedSubscription.flow_used >= selectedSubscription.flow_total" tone="warning" title="流量已用完">{{ exhaustedMessage(selectedSubscription) }}<UiButton v-if="canReset(selectedSubscription)" as-child variant="ghost" size="sm"><RouterLink :to="`/account/plans?operation=reset&subscription=${selectedSubscription.id}`">购买流量重置</RouterLink></UiButton></PageAlert>
        <p v-else-if="!accessReady" role="status">{{ working ? '正在更新链接状态…' : '正在读取链接状态…' }}</p>
        <div v-else-if="!access.configured" class="access-meta"><strong>{{ access.notice || '此订阅尚未生成可用链接' }}</strong></div>

        <div class="subscription-access-context">
          <div><span>剩余流量</span><strong>{{ formatBytes(Math.max(0, selectedSubscription.flow_total - selectedSubscription.flow_used)) }}</strong></div>
          <div><span>到期时间</span><TimeBadge :value="selectedSubscription.end_at" /></div>
        </div>

        <div v-if="subscriptionUrl" class="access-template-picker">
          <FormField v-slot="{ controlAttrs }" label="客户端格式" name="subscription-format">
            <UiSelect v-model="selectedTemplate" v-bind="controlAttrs" :options="templateOptions" />
          </FormField>
          <WorkbenchFilterInput v-model="templateDraftQuery" label="查找格式" placeholder="客户端或模板名称" @apply="searchTemplates" />
          <span v-if="templateTotal > templates.length" class="field-hint">{{ templates.length }} / {{ templateTotal }}</span>
        </div>
        <FormField v-if="subscriptionUrl" v-slot="{ controlAttrs }" class="access-url" label="订阅链接" full>
          <UiInput :value="subscriptionUrl" v-bind="controlAttrs" readonly />
        </FormField>
        <details class="subscription-options"><summary>链接管理</summary><div class="subscription-options-body"><p v-if="deliveryHint" class="muted">{{ deliveryHint }}</p>
        <div v-if="accessReady && access.configured" class="access-meta">
          <p>凭证前缀 <code>{{ access.token_prefix }}…</code></p>
          <div class="access-time"><span>最近使用</span><TimeBadge :value="access.last_used_at" /></div>
        </div>
        <div class="access-actions">
          <UiButton v-if="accessReady && access.configured" variant="secondary" size="sm" type="button" :disabled="access.delivery_configured === false || !selectedContextReady || working || selectedSubscription.flow_used >= selectedSubscription.flow_total" @click="ask('rotate')">
            重置链接
          </UiButton>
          <UiButton v-if="accessReady && access.configured" variant="danger" size="sm" type="button" :disabled="working || !selectedContextReady" @click="ask('revoke')">停用链接</UiButton>
        </div>
        </div></details>
      </div>
      <EmptyState v-else icon="key" title="请选择有效订阅" description="每条订阅分别生成和管理自己的客户端链接。" />
    </UiSection>

    </div>

    <DataWorkbench :total="total" :loading="listLoading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(status)" @clear="clearFilters">
          <WorkbenchFilterSelect v-model="status" label="订阅状态" :options="statusOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <TableSkeleton v-if="listLoading && !subscriptions.length" label="正在加载我的订阅" :columns="5" />
      <div v-else-if="subscriptions.length" class="account-service-grid">
        <AccountSubscriptionCard v-for="sub in subscriptions" :key="sub.id" :subscription="sub" :selected="sub.id === selectedSubscriptionID">
          <template #name><UiButton variant="ghost" class="subscription-name-button" :disabled="sub.status !== 'active' || working" @click="selectSubscription(sub.id)">{{ sub.plan_name || `订阅 #${sub.id}` }}</UiButton></template>
          <template #notice><div v-if="automaticNeedsAttention(sub.automatic_renewal_state)" class="subscription-renewal-result"><UiButton variant="ghost" size="sm" @click="automaticTarget = { id: sub.id, planId: sub.plan_id }"><UiIcon name="alert" />{{ sub.automatic_renewal_state === 'insufficient_balance' ? '自动续费余额不足' : '自动续费待核验' }}</UiButton></div></template>
          <template #actions>
            <RowActions :label="`${sub.plan_name || `订阅 #${sub.id}`}的更多操作`"><template #primary>
              <UiButton variant="secondary" size="sm" :disabled="sub.status !== 'active' || working" @click="selectSubscription(sub.id)">{{ sub.flow_used >= sub.flow_total ? '管理订阅' : sub.id === selectedSubscriptionID ? '查看链接' : '获取订阅' }}</UiButton>
              <UiButton v-if="sub.can_renew" as-child variant="secondary" size="sm"><RouterLink :to="`/account/plans?operation=renew&subscription=${sub.id}`">{{ sub.renewal_until ? '续费恢复' : '续费' }}</RouterLink></UiButton>
            </template>
              <UiButton v-if="canReset(sub)" as-child variant="ghost" size="sm"><RouterLink :to="`/account/plans?operation=reset&subscription=${sub.id}`">购买流量重置</RouterLink></UiButton>
              <UiButton v-if="['active', 'expired'].includes(sub.status)" variant="ghost" size="sm" @click="terminationTarget = sub.id">退订</UiButton>
              <UiButton variant="ghost" size="sm" @click="automaticTarget = { id: sub.id, planId: sub.plan_id }">自动续费</UiButton>
              <UiButton v-if="Number.isSafeInteger(sub.id) && sub.id > 0" as-child variant="ghost" size="sm"><RouterLink :to="{ path: '/account/tickets', query: { new: '1', subscription_id: String(sub.id) } }">提交工单</RouterLink></UiButton>
            </RowActions>
          </template>
        </AccountSubscriptionCard>
      </div>
      <EmptyState v-else-if="!listLoading && !listError" icon="plans" title="暂无订阅" description="" />

      <template #footer>
        <TablePager
          :total="total"
          :offset="offset"
          :limit="limit"
          :loading="listLoading"
          @change="changePage"
        />
      </template>
    </DataWorkbench>

    <div v-if="terminationTarget" ref="terminationPanel" tabindex="-1"><SubscriptionTermination :subscription-id="terminationTarget" :subscription-name="subscriptions.find(sub => sub.id === terminationTarget)?.plan_name" @settled="loadAll" /></div>
    <div v-if="automaticTarget" ref="automaticPanel" tabindex="-1"><SubscriptionAutomaticRenewal :subscription-id="automaticTarget.id" :plan-id="automaticTarget.planId" :subscription-name="subscriptions.find(sub => sub.id === automaticTarget?.id)?.plan_name" @changed="refreshAutomaticRenewal" /></div>

    <details class="subscription-diagnostics"><summary>查看线路运行情况</summary>
    <UiSection variant="outlined" title="线路状态">
      <template #meta><RemoteReadStatus compact label="线路运行情况" :phase="protocolLoadResource.phase.value" :error="protocolLoadResource.error.value" :last-success-at="protocolLoadResource.lastSuccessAt.value" @retry="loadProtocolLoads" /></template>
      <div class="panel-body">
        <PageAlert v-if="protocolLoadResource.error.value" tone="danger" title="协议实时负载加载失败">
          {{ protocolLoadResource.error.value }} 订阅链接仍可独立使用。
          <UiButton variant="secondary" size="sm" type="button" @click="loadProtocolLoads">重试负载</UiButton>
        </PageAlert>
        <p v-if="protocolLoadResource.loading.value && !protocolLoadResource.loaded.value" role="status">正在读取协议实时负载…</p>
        <DataTable v-if="protocolLoads.items.length" caption="可用协议实时负载" :row-count="protocolLoads.items.length" :min-width="720">
          <thead><tr><th class="table-primary-column">使用位置</th><th>协议</th><th class="numeric-column">活跃用户</th><th class="numeric-column">活跃连接</th><th data-column-priority="2">负载更新</th></tr></thead>
          <tbody>
            <tr v-for="item in protocolLoads.items" :key="item.protocol_endpoint_id">
              <td class="table-primary-column"><div class="cell-title"><strong>{{ item.name }}</strong><TableText :value="item.region || '未设置区域'" /><StatusBadge :tone="item.connector_online ? 'success' : 'warning'" icon="activity">{{ item.connector_online ? '上报正常' : '上报延迟' }}</StatusBadge></div></td>
              <td>{{ item.protocol.toUpperCase() }}</td>
              <td class="numeric-column">{{ item.load_fresh ? item.active_users : '—' }}</td>
              <td class="numeric-column">{{ item.load_fresh ? item.active_flows : '—' }}</td>
              <td data-column-priority="2"><TimeBadge v-if="item.load_fresh" :value="item.load_observed_at" /><span v-else>{{ item.load_observed_at ? '负载数据已过期' : '暂无负载数据' }}</span></td>
            </tr>
          </tbody>
        </DataTable>
        <EmptyState v-else-if="protocolLoadResource.loaded.value && !protocolLoadResource.error.value && !protocolLoadResource.loading.value" icon="activity" title="暂无可用协议" description="当前没有有效套餐授权的协议。节点上报延迟不会影响已授权协议的展示。" />
      </div>
    </UiSection>
    </details>




    <ConfirmDialog
      :open="confirmOpen"
      :title="confirmTitle"
      :message="confirmMessage"
      :confirm-text="confirmText"
      :tone="kind === 'revoke' ? 'danger' : 'primary'"
      :busy="working"
      @close="confirmOpen = false"
      @confirm="execute"
    />
  </section>
</template>

<script setup lang="ts">
import AccountSubscriptionCard from '../../components/account/AccountSubscriptionCard.vue'
import PageRefreshButton from '../../components/PageRefreshButton.vue'
import WorkbenchFilterInput from '../../components/WorkbenchFilterInput.vue'
import SubscriptionTermination from '../../components/SubscriptionTermination.vue'
import SubscriptionAutomaticRenewal from '../../components/SubscriptionAutomaticRenewal.vue'
import PluginSlot from '../../plugins/PluginSlot.vue'
import RemoteReadStatus from '../../components/RemoteReadStatus.vue'
import RowActions from '../../components/RowActions.vue'
import TableText from '../../components/TableText.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '../../stores/app'
import { isAuthSessionExpired } from '../../utils/authSession'
import {
  fetchActiveSubscriptionTemplatesPage,
  fetchAccountProtocolLoads,
  fetchAccountSubscriptionsPage,
  getAuthToken,
  type AccountProtocolLoadSnapshot,
  type AdminSubscriptionListItem,
  type SubscriptionTemplate,
} from '../../api/client'
import {
  fetchSubscriptionAccess,
  revokeSubscriptionAccess,
  rotateSubscriptionAccess,
  type SubscriptionAccess,
} from '../../api/subscriptionAccess'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
import DataTable from '../../components/DataTable.vue'
import TableSkeleton from '../../components/TableSkeleton.vue'
import DataWorkbench from '../../components/DataWorkbench.vue'
import EmptyState from '../../components/EmptyState.vue'
import FormField from '../../components/FormField.vue'
import PageAlert from '../../components/PageAlert.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TablePager from '../../components/TablePager.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import UiInput from '../../components/UiInput.vue'
import UiSection from '../../components/UiSection.vue'
import UiSelect from '../../components/UiSelect.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterSelect from '../../components/WorkbenchFilterSelect.vue'
import { useRemoteTable } from '../../composables/useRemoteTable'
import { useRemoteResource } from '../../composables/useRemoteResource'
import { formatBytes } from '../../utils/format'
import {
  buildSubscriptionURL,
  isBuiltInDeliveryMode,
  subscriptionDeliveryAuto,
  subscriptionDeliveryNative,
} from '../../utils/subscriptionDelivery'

const accessPanel = ref<HTMLElement | null>(null)
const automaticPanel = ref<HTMLElement | null>(null), terminationPanel = ref<HTMLElement | null>(null)

const terminationTarget = ref(0)
const automaticTarget = ref<{ id: number; planId: number } | null>(null)
watch([automaticTarget, terminationTarget], async ([automatic, termination], previous) => {
  await nextTick()
  const panel = automatic !== previous?.[0] && automatic ? automaticPanel.value : termination ? terminationPanel.value : null
  panel?.focus({ preventScroll: true }); panel?.scrollIntoView?.({ block: 'start' })
})
function automaticNeedsAttention(state?: string) {
  return Boolean(state && !['waiting', 'renewed', 'disabled', 'window_closed'].includes(state))
}
const app = useAppStore()
const identityScope = () => `${app.user?.id || 0}:${app.sessionEpoch}:${getAuthToken()}`
const selectedNotice = ref('')
const selectedUnavailable = ref(false)
const automaticRowError = ref('')
let automaticRowController: AbortController | null = null
const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 25)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const status = ref(String(route.query.status || ''))
const selectedSubscriptionID = ref(Math.max(0, Number(route.query.subscription) || 0))
const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '有效', value: 'active' },
  { label: '已到期', value: 'expired' },
  { label: '已取消', value: 'canceled' },
]
const templateQuery = ref('')
const templateDraftQuery = ref('')
const selectedTemplate = ref(subscriptionDeliveryAuto)
const copyLabel = ref('复制链接')
const working = ref(false)
const message = ref('')
const confirmOpen = ref(false)
const kind = ref<'generate' | 'rotate' | 'revoke'>('generate')
let selectionGeneration = 0
let disposed = false
let mutationSubscriptionID = 0
const protocolLoadRefreshIntervalMS = 15_000
let protocolLoadRefreshTimer: number | undefined

const {
  items: subscriptions,
  total,
  loading: listLoading,
  refreshing,
  error: listError,
  load,
  reset: resetSubscriptions,
} = useRemoteTable<AdminSubscriptionListItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchAccountSubscriptionsPage({
    status: status.value || undefined,
    offset: offset.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '订阅列表加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

const selectedSubscription = computed(() => {
  if (selectedUnavailable.value) return null
  if (selectedSubscriptionResource.loaded.value) {
    const selected = selectedSubscriptionResource.data.value
    return selected?.id === selectedSubscriptionID.value && selected.status === 'active' ? selected : null
  }
  if (selectedSubscriptionResource.errorKind.value === 'forbidden'
    || selectedSubscriptionResource.errorKind.value === 'unauthorized' && !isAuthSessionExpired()) return null
  return activeSubscriptions.value.find(item => item.id === selectedSubscriptionID.value)
    || subscriptions.value.find(item => item.id === selectedSubscriptionID.value && item.status === 'active')
    || null
})

const selectedSubscriptionResource = useRemoteResource<AdminSubscriptionListItem | null>({
  initial: () => null,
  fetch: async ({ signal }) => {
    const id = selectedSubscriptionID.value, selection = selectionGeneration, identity = identityScope()
    const isCurrent = () => !signal.aborted && !disposed && id === selectedSubscriptionID.value
      && selection === selectionGeneration && identity === identityScope()
    try {
      const page = await fetchAccountSubscriptionsPage({ subscriptionId: id, status: 'active', offset: 0, limit: 1 }, { signal })
      if (!isCurrent()) throw Object.assign(new Error('subscription identity changed'), { code: 'AUTH_SESSION_CHANGED' })
      const selected = page.items.find(item => item.id === id && item.status === 'active') || null
      selectedUnavailable.value = !selected
      return selected
    } catch (cause: any) {
      if (!isCurrent()) throw Object.assign(new Error('subscription identity changed'), { code: 'AUTH_SESSION_CHANGED' })
      // A confirmed missing target is authoritative; a network error is unknown.
      // Do not let the cached list turn a 404 into another available credential.
      if (cause?.response?.status === 404) { selectedUnavailable.value = true; return null }
      if (cause?.response?.status === 403) selectedUnavailable.value = true
      throw cause
    }
  },
  errorMessage: '暂时无法刷新所选订阅，请核对网络后重试；已读取的信息可能已过期。',
})
const selectedContextReady = computed(() => selectedSubscriptionResource.loaded.value
  && !selectedSubscriptionResource.loading.value && !selectedSubscriptionResource.error.value
  && Boolean(selectedSubscription.value))

const activeSubscriptionResource = useRemoteResource({
  initial: () => ({ items: [] as AdminSubscriptionListItem[], total: 0 }),
  fetch: async ({ signal }) => {
    const page = await fetchAccountSubscriptionsPage({ status: 'active', offset: 0, limit: 1 }, { signal })
    return { items: page.items, total: page.total }
  },
  errorMessage: '有效订阅加载失败。',
})
const templateResource = useRemoteResource({
  initial: () => ({ items: [] as SubscriptionTemplate[], total: 0 }),
  fetch: async ({ signal }) => {
    const page = await fetchActiveSubscriptionTemplatesPage({ q: templateQuery.value || undefined, offset: 0, limit: 25 }, { signal })
    return { items: page.items, total: page.total }
  },
  errorMessage: '订阅模板加载失败。',
})
const protocolLoadResource = useRemoteResource<AccountProtocolLoadSnapshot>({
  initial: () => ({ sampled_at: '', activity_window_seconds: 120, items: [] }),
  fetch: ({ signal }) => fetchAccountProtocolLoads({ signal }),
  errorMessage: '协议实时负载加载失败。',
})
const accessResource = useRemoteResource<SubscriptionAccess>({
  initial: () => ({ configured: false, subscription_id: selectedSubscriptionID.value }),
  fetch: async ({ signal }) => {
    const subscriptionID = selectedSubscriptionID.value, selection = selectionGeneration, identity = identityScope()
    const result = await fetchSubscriptionAccess(subscriptionID, { signal })
    if (signal.aborted || disposed || selection !== selectionGeneration || identity !== identityScope()) {
      throw Object.assign(new Error('subscription identity changed'), { code: 'AUTH_SESSION_CHANGED' })
    }
    if (result.subscription_id !== subscriptionID) throw new Error('subscription access response mismatch')
    return result
  },
  errorMessage: '订阅链接加载失败。',
})

const activeSubscriptions = computed(() => activeSubscriptionResource.data.value.items)
const templates = computed(() => templateResource.data.value.items)
const templateTotal = computed(() => templateResource.data.value.total)
const templateLoading = templateResource.loading
const metadataLoading = computed(() => activeSubscriptionResource.loading.value || selectedSubscriptionResource.loading.value)
const protocolLoads = protocolLoadResource.data
const access = accessResource.data
const accessLoading = accessResource.loading
const accessReady = computed(() => Boolean(selectedSubscription.value) && accessResource.loaded.value
  && !accessLoading.value
  && !accessResource.error.value
  && access.value.subscription_id === selectedSubscriptionID.value)
const loading = computed(() => listLoading.value || metadataLoading.value || accessLoading.value || templateLoading.value || protocolLoadResource.loading.value)
const error = computed(() => listError.value || automaticRowError.value || selectedNotice.value || selectedSubscriptionResource.error.value || activeSubscriptionResource.error.value || templateResource.error.value)
const subscriptionUrl = computed(() => accessReady.value
  ? buildSubscriptionURL(access.value.subscription_url ? new URL(access.value.subscription_url, window.location.origin).toString() : '', selectedTemplate.value)
  : '')
const templateOptions = computed(() => [
  { label: '自动识别（推荐）', value: subscriptionDeliveryAuto },
  { label: 'ZBoard 原生格式（Base64）', value: subscriptionDeliveryNative },
  ...templates.value
    .filter(item => !isBuiltInDeliveryMode(item.slug))
    .map(item => ({ label: item.name, value: item.slug })),
])
const deliveryHint = computed(() => {
  if (templateTotal.value > templates.value.length) {
    return `当前显示 ${templates.value.length} 项，共 ${templateTotal.value} 项；可通过搜索定位其他模板。`
  }
  if (selectedTemplate.value === subscriptionDeliveryAuto) {
    return '服务端按 User-Agent 自动选择 ZNet Sink、Clash/Mihomo 或 sing-box；无法识别时返回 Base64 编码的 ZBoard 原生配置。'
  }
  if (selectedTemplate.value === subscriptionDeliveryNative) {
    return '始终返回 Base64 编码的 ZBoard 原生配置，不受客户端 User-Agent 影响。'
  }
  return '显式选择只改变配置正文，不改变当前订阅的凭证、节点范围或流量计费。'
})
const selectedLabel = computed(() => selectedSubscription.value ? `订阅 #${selectedSubscription.value.id}` : '当前订阅')
const confirmTitle = computed(() => kind.value === 'generate' ? `生成${selectedLabel.value}的链接？` : kind.value === 'rotate' ? `轮换${selectedLabel.value}的链接？` : `吊销${selectedLabel.value}的链接？`)
const confirmText = computed(() => kind.value === 'generate' ? '生成链接' : kind.value === 'rotate' ? '确认轮换' : '确认吊销')
const confirmMessage = computed(() => kind.value === 'generate'
  ? `生成后只有${selectedLabel.value}会使用这条链接。`
  : kind.value === 'rotate'
    ? '旧链接会立即失效，但不会影响账户下其他订阅。'
    : `吊销后只有${selectedLabel.value}无法继续拉取配置。`)

function canReset(sub: AdminSubscriptionListItem) {
  return Boolean(sub.can_reset)
}

function exhaustedMessage(sub: AdminSubscriptionListItem) {
  if (sub.can_reset) return '订阅尚未到期，可以购买重置流量恢复额度。'
  if (sub.next_reset_at && new Date(sub.next_reset_at).getTime() > Date.now() && new Date(sub.next_reset_at).getTime() < new Date(sub.end_at).getTime() && (sub.reset_policy || 0) > 0) return '订阅尚未到期，流量将在下次自动重置时恢复。'
  if (sub.can_addon) return '此订阅流量已用完，可前往套餐中心购买流量包。'
  if (sub.can_renew) return '此订阅流量已用完，可前往套餐中心续费或购买新的订阅。'
  return '此订阅流量已用完，可前往套餐中心购买新的订阅。'
}

function selectDefaultSubscription() {
  if (selectedSubscriptionID.value) return
  selectedSubscriptionID.value = activeSubscriptions.value[0]?.id || 0
}

async function loadAccess() {
  if (working.value && mutationSubscriptionID === selectedSubscriptionID.value) return
  if (!selectedSubscriptionID.value) return
  const selection = selectionGeneration, identity = identityScope()
  const isCurrent = () => !disposed && selection === selectionGeneration && identity === identityScope()
  // The selected target can be outside both the list page and the one-row active
  // summary. Its authoritative quota/term GET must run for every explicit read.
  const ready = await selectedSubscriptionResource.load()
  if (!isCurrent()) return
  if (!ready) {
    if (selectedSubscriptionResource.errorKind.value === 'forbidden'
      || selectedSubscriptionResource.errorKind.value === 'unauthorized' && !isAuthSessionExpired()) accessResource.reset()
    return
  }
  if (!selectedSubscriptionResource.data.value) {
    accessResource.reset()
    selectedNotice.value = '所选订阅不存在或已失效，请从列表重新选择。'
    return
  }
  selectedNotice.value = ''
  await accessResource.load()
}

async function loadMetadata() {
  const selection = selectionGeneration, identity = identityScope()
  // A slow/failed active-count summary cannot hold an explicit selected target.
  if (selectedSubscriptionID.value) {
    await Promise.all([activeSubscriptionResource.load(), loadAccess()])
  } else if (await activeSubscriptionResource.load()) {
    if (disposed || selection !== selectionGeneration || identity !== identityScope()) return
    selectDefaultSubscription()
    await loadAccess()
  }
  if (!disposed && identity === identityScope()) await syncURL(true)
}

async function refreshAutomaticRenewal(subscriptionID: number) {
  if (disposed || automaticTarget.value?.id !== subscriptionID) return
  automaticRowController?.abort()
  const controller = new AbortController()
  automaticRowController = controller
  const identity = identityScope(), pageOffset = offset.value, pageLimit = limit.value, filter = status.value
  automaticRowError.value = ''
  // Refresh only the affected row and, when it is still managed, its exact
  // access context. Optional statistics/counts and other subscriptions stay independent.
  const detail = selectedSubscriptionID.value === subscriptionID ? loadAccess() : Promise.resolve()
  try {
    const page = await fetchAccountSubscriptionsPage({ subscriptionId: subscriptionID, offset: 0, limit: 1 }, { signal: controller.signal })
    if (disposed || controller.signal.aborted || identity !== identityScope()
      || pageOffset !== offset.value || pageLimit !== limit.value || filter !== status.value) return
    const row = page.items.find(item => item.id === subscriptionID)
    if (row) subscriptions.value = subscriptions.value.map(item => item.id === subscriptionID ? row : item)
    else automaticRowError.value = '自动续费设置已保存，但该订阅的列表状态暂无法核对，请刷新列表。'
  } catch {
    if (!disposed && !controller.signal.aborted && identity === identityScope()) {
      automaticRowError.value = '自动续费设置已保存，列表仍显示上次读取的状态，请刷新后核对。'
    }
  } finally {
    if (automaticRowController === controller) automaticRowController = null
    await detail
  }
}

async function searchTemplates() {
  templateQuery.value = templateDraftQuery.value.trim()
  if (await templateResource.load()) {
    if (!isBuiltInDeliveryMode(selectedTemplate.value) && !templates.value.some(item => item.slug === selectedTemplate.value)) {
      selectedTemplate.value = subscriptionDeliveryAuto
    }
  }
}

async function loadProtocolLoads() { await protocolLoadResource.load() }

async function loadAll() {
  await Promise.all([load(), loadMetadata(), searchTemplates(), loadProtocolLoads()])
}

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = {
    query: {
      ...(status.value ? { status: status.value } : {}),
      ...(page > 1 ? { page: String(page) } : {}),
      ...(limit.value !== 25 ? { limit: String(limit.value) } : {}),
      ...(selectedSubscriptionID.value ? { subscription: String(selectedSubscriptionID.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
}

async function selectSubscription(subscriptionID: number) {
  if (working.value) return
  if (subscriptionID === selectedSubscriptionID.value) { focusAccess(); return }
  selectedSubscriptionID.value = subscriptionID
  message.value = ''
  const settled = Promise.all([syncURL(), loadAccess()])
  await nextTick()
  focusAccess()
  await settled
}
function focusAccess() {
  accessPanel.value?.focus({ preventScroll: true })
  accessPanel.value?.scrollIntoView?.({ block: 'start' })
}

async function applyFilters() {
  offset.value = 0
  await syncURL()
  await load()
}

async function clearFilters() {
  status.value = ''
  await applyFilters()
}

async function changePage(value: { offset: number; limit: number }) {
  offset.value = value.offset
  limit.value = value.limit
  await syncURL()
  await load()
}

function ask(next: typeof kind.value) {
  if (!selectedSubscription.value || !selectedContextReady.value || !accessReady.value || working.value) return
  kind.value = next
  confirmOpen.value = true
}

async function execute() {
  if (!selectedSubscription.value || !selectedContextReady.value || !accessReady.value || working.value || !confirmOpen.value) return
  const subscriptionID = selectedSubscription.value.id
  const selection = selectionGeneration
  const action = kind.value, identity = identityScope()
  const isCurrent = () => !disposed && identity === identityScope() && selection === selectionGeneration && subscriptionID === selectedSubscriptionID.value
  mutationSubscriptionID = subscriptionID
  working.value = true
  accessResource.reset()
  message.value = ''
  try {
    if (action === 'revoke') {
      await revokeSubscriptionAccess(subscriptionID)
      if (!isCurrent()) return
      accessResource.replace({ configured: false, subscription_id: subscriptionID })
      message.value = `订阅 #${subscriptionID} 的链接已吊销。`
    } else {
      const result = await rotateSubscriptionAccess(subscriptionID)
      if (!isCurrent()) return
      if (result.subscription_id !== subscriptionID) throw new Error('subscription access response mismatch')
      accessResource.replace(result)
      copyLabel.value = '复制链接'
      message.value = action === 'rotate'
        ? `订阅 #${subscriptionID} 的链接已轮换。`
        : `订阅 #${subscriptionID} 的链接已生成。`
    }
    confirmOpen.value = false
  } catch (cause: any) {
    if (isCurrent()) {
      accessResource.error.value = cause?.response?.data?.message || '链接操作失败，请重新读取状态后再试。'
      confirmOpen.value = false
    }
  } finally {
    if (!disposed) {
      working.value = false
      // Returning to the original subscription during its mutation deferred its
      // GET. Read the settled state now; never reuse the old selection's result.
      if (selection !== selectionGeneration && subscriptionID === selectedSubscriptionID.value) await loadAccess()
    }
  }
}

async function copy() {
  if (!subscriptionUrl.value) return
  const selection = selectionGeneration
  try {
    await navigator.clipboard.writeText(subscriptionUrl.value)
    if (!disposed && selection === selectionGeneration) copyLabel.value = '已复制'
  } catch {
    if (!disposed && selection === selectionGeneration) copyLabel.value = '复制失败'
  }
}

// Clear credentials synchronously, before URL navigation or a replacement GET.
watch(selectedSubscriptionID, () => {
  selectionGeneration += 1
  selectedSubscriptionResource.reset()
  selectedNotice.value = ''
  selectedUnavailable.value = false
  accessResource.reset()
  copyLabel.value = '复制链接'
  message.value = ''
  confirmOpen.value = false
}, { flush: 'sync' })

// Changing account/permission epoch invalidates every cached personal read and
// in-flight continuation, even if this route remains mounted. Read recovery is
// separate from mutations: no credential or commerce write is ever replayed.
watch([() => app.user?.id || 0, () => app.sessionEpoch], () => {
  selectionGeneration += 1
  const selection = selectionGeneration
  automaticRowController?.abort()
  automaticRowController = null
  automaticRowError.value = ''
  resetSubscriptions()
  activeSubscriptionResource.reset()
  selectedSubscriptionResource.reset()
  accessResource.reset()
  templateResource.reset()
  protocolLoadResource.reset()
  selectedNotice.value = ''
  selectedUnavailable.value = false
  terminationTarget.value = 0
  automaticTarget.value = null
  confirmOpen.value = false
  message.value = ''
  copyLabel.value = '复制链接'
  void Promise.resolve().then(() => {
    if (!disposed && selection === selectionGeneration && app.user?.id && !app.reauthRequired) void loadAll()
  })
}, { flush: 'sync' })

watch(() => route.fullPath, async () => {
  const nextStatus = String(route.query.status || '')
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 25
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  const nextSubscriptionID = Math.max(0, Number(route.query.subscription) || 0)
  const listChanged = nextStatus !== status.value || nextLimit !== limit.value || nextOffset !== offset.value
  const subscriptionChanged = nextSubscriptionID !== selectedSubscriptionID.value
  if (subscriptionChanged) selectedSubscriptionID.value = nextSubscriptionID
  if (listChanged) {
    status.value = nextStatus
    limit.value = nextLimit
    offset.value = nextOffset
  }
  await Promise.all([listChanged ? load() : Promise.resolve(), subscriptionChanged ? loadAccess() : Promise.resolve()])
})

onMounted(() => {
  void loadAll()
  protocolLoadRefreshTimer = window.setInterval(() => {
    if (!protocolLoadResource.loading.value) void loadProtocolLoads()
  }, protocolLoadRefreshIntervalMS)
})
onBeforeUnmount(() => {
  automaticRowController?.abort()
  disposed = true
  selectionGeneration += 1
  if (protocolLoadRefreshTimer !== undefined) window.clearInterval(protocolLoadRefreshTimer)
})
</script>

<style scoped>
.subscription-name-button { justify-content: flex-start; height: auto; min-height: 0; padding: 0; white-space: normal; text-align: left; overflow-wrap: anywhere; font-size: 16px; font-weight: 600; }
.subscription-name-button:disabled { opacity: 1; color: var(--foreground); }
.access-template-picker { display: flex; align-items: flex-end; flex-wrap: wrap; gap: 12px; }
.access-template-picker > .form-field { flex: 1; min-width: min(100%, 240px); max-width: 480px; }
.subscription-renewal-result { color: var(--destructive); }

.subscription-access-panel { scroll-margin-top: 100px; }
.subscription-access-panel:focus-visible { outline: 2px solid var(--ring); outline-offset: 4px; }
.subscription-options, .subscription-diagnostics { padding-top: 12px; }
.subscription-options summary, .subscription-diagnostics summary { width: fit-content; cursor: pointer; font-size: 13px; color: var(--foreground); }
.subscription-options summary:focus-visible, .subscription-diagnostics summary:focus-visible { outline: 2px solid var(--ring); outline-offset: 4px; }
.subscription-options-body { display: grid; gap: 16px; padding-top: 16px; }
.subscription-diagnostics > .ui-section { margin-top: 12px; }

.quota-cell {
  min-width: 180px;
}

.quota-cell > span,
.quota-cell small {
  display: block;
  font-size: 12px;
}

.quota-cell small {
  margin-top: 6px;
  color: var(--muted);
}

.quota-cell .usage-track {
  height: 5px;
  margin-top: 7px;
}



.selected-subscription-row td {
  background: var(--primary-soft);
}

.subscription-access-context {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  max-width: 640px;
}

.subscription-access-context > div {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.subscription-access-context span {
  color: var(--muted);
  font-size: 12px;
}

.subscription-access-context strong {
  font-size: 13px;
}

.access-template-picker { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px; }

.inline-filter {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

@media (max-width: 760px) {
  .subscription-access-context { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .inline-filter { grid-template-columns: 1fr; }
  .access-template-picker > .form-field { min-width: 0; flex-basis: 170px; }
}
</style>
