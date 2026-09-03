<template>
  <section class="account-page stack">
    <PageHeader title="订阅配置" description="每个服务实例拥有独立链接，分别管理客户端配置和访问凭证。" eyebrow="SUBSCRIPTION">
      <template #actions>
        <UiButton variant="secondary" type="button" :disabled="loading || working" @click="loadAll">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <TransientFeedback
      :success="message"
      :error="error"
      success-title="订阅配置已更新"
      error-title="订阅操作失败"
    />

    <DataWorkbench :total="total" :loading="listLoading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(status)" @clear="clearFilters">
          <WorkbenchFilterSelect v-model="status" label="订阅状态" :options="statusOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions>
        <StatusBadge :tone="activeSubscriptionTotal ? 'success' : 'neutral'">
          {{ activeSubscriptionTotal }} 个有效订阅
        </StatusBadge>
      </template>

      <DataTable v-if="subscriptions.length" caption="我的订阅列表" :row-count="total" :min-width="940">
        <thead>
          <tr>
            <th class="table-primary-column">订阅</th>
            <th>状态</th>
            <th data-column-priority="2">套餐规格</th>
            <th>流量配额</th>
            <th data-column-priority="1">到期时间</th>
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="sub in subscriptions" :key="sub.id" :class="{ 'selected-subscription-row': sub.id === selectedSubscriptionID }">
            <td class="table-primary-column"><strong class="mono">#{{ sub.id }}</strong></td>
            <td><StatusBadge :tone="subTone(sub.status)">{{ subLabel(sub.status) }}</StatusBadge></td>
            <td data-column-priority="2">
              <div class="cell-title">
                <strong>{{ sub.plan_name || `套餐 #${sub.plan_id}` }}</strong>
                <span>{{ sub.sku_name || `SKU #${sub.plan_sku_id}` }}</span>
              </div>
            </td>
            <td>
              <div class="quota-cell">
                <span>{{ formatBytes(sub.flow_used) }} / {{ formatBytes(sub.flow_total) }}</span>
                <div class="usage-track"><i :style="{ width: `${percent(sub)}%` }"></i></div>
                <small>剩余 {{ formatBytes(Math.max(0, sub.flow_total - sub.flow_used)) }}</small>
              </div>
            </td>
            <td data-column-priority="1"><TimeBadge :value="sub.end_at" /></td>
            <td class="table-action-column">
              <div class="subscription-row-actions">
                <UiButton
                  variant="secondary"
                  size="sm"
                  type="button"
                  :disabled="sub.status !== 'active' || working"
                  @click="selectSubscription(sub.id)"
                >
                  {{ sub.id === selectedSubscriptionID ? '正在管理' : '管理链接' }}
                </UiButton>
                <RouterLink class="button button-secondary button-sm" :to="`/account/plans?operation=renew&subscription=${sub.id}`">续费</RouterLink>
              </div>
            </td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else icon="plans" title="暂无订阅" description="订单确认后，订阅会自动显示在这里。" />

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

    <UiSection title="协议实时负载" description="仅显示当前有效套餐可使用的协议；活跃人数和连接数来自最近两分钟的 Zero 会话事件。">
      <template #meta><TimeBadge :value="protocolLoads.sampled_at" /></template>
      <div class="panel-body">
        <PageAlert v-if="protocolLoadResource.error.value" tone="danger" title="协议实时负载加载失败">
          {{ protocolLoadResource.error.value }} 订阅链接仍可独立使用。
          <UiButton variant="secondary" size="sm" type="button" @click="loadProtocolLoads">重试负载</UiButton>
        </PageAlert>
        <p v-if="protocolLoadResource.loading.value" role="status">正在读取协议实时负载…</p>
        <DataTable v-if="protocolLoads.items.length" caption="可用协议实时负载" :row-count="protocolLoads.items.length" :min-width="720">
          <thead><tr><th class="table-primary-column">使用位置</th><th>协议</th><th class="numeric-column">活跃用户</th><th class="numeric-column">活跃连接</th><th data-column-priority="2">最近活动</th></tr></thead>
          <tbody><tr v-for="item in protocolLoads.items" :key="item.protocol_endpoint_id"><td class="table-primary-column"><div class="cell-title"><strong>{{ item.name }}</strong><span>{{ item.region || '未设置区域' }}</span></div></td><td><StatusBadge :tone="item.active_flows ? 'success' : 'neutral'" icon="activity">{{ item.protocol.toUpperCase() }}</StatusBadge></td><td class="numeric-column">{{ item.active_users }}</td><td class="numeric-column">{{ item.active_flows }}</td><td data-column-priority="2"><TimeBadge :value="item.last_activity_at" /></td></tr></tbody>
        </DataTable>
        <EmptyState v-else-if="protocolLoadResource.loaded.value && !protocolLoadResource.error.value && !protocolLoadResource.loading.value" icon="activity" title="暂无可用协议" description="拥有有效套餐且节点在线后，会在这里显示对应协议的实时使用人数和连接数。" />
      </div>
    </UiSection>

    <UiSection
      :title="selectedSubscription ? `订阅 #${selectedSubscription.id} 的访问链接` : '订阅访问链接'"
      :description="selectedSubscription ? `${selectedSubscription.plan_name} · ${selectedSubscription.sku_name}` : '从订阅列表选择需要管理的服务实例。'"
    >
      <template #meta>
        <StatusBadge v-if="selectedSubscription && accessReady" :tone="access.configured ? 'success' : 'warning'">
          {{ access.configured ? '已启用' : '未生成' }}
        </StatusBadge>
      </template>
      <div v-if="selectedSubscription" class="panel-body access-panel">
        <span class="access-icon"><UiIcon name="key" /></span>
        <PageAlert v-if="accessResource.error.value" tone="danger" title="订阅链接读取失败">
          {{ accessResource.error.value }}
          <UiButton variant="secondary" size="sm" type="button" @click="loadAccess">重试链接</UiButton>
        </PageAlert>
        <p v-else-if="!accessReady" role="status">{{ working ? '正在更新链接状态…' : '正在读取链接状态…' }}</p>
        <div v-else-if="access.configured" class="access-meta">
          <strong>{{ selectedSubscription.plan_name }} 的链接已启用</strong>
          <p>凭证前缀 <code>{{ access.token_prefix }}…</code></p>
          <div class="access-time"><span>最近使用</span><TimeBadge :value="access.last_used_at" /></div>
        </div>
        <div v-else class="access-meta">
          <strong>此订阅尚未生成可用链接</strong>
          <p>生成或轮换只会影响订阅 #{{ selectedSubscription.id }}。</p>
        </div>

        <div class="subscription-access-context">
          <div><span>套餐规格</span><strong>{{ selectedSubscription.plan_name }} · {{ selectedSubscription.sku_name }}</strong></div>
          <div><span>剩余流量</span><strong>{{ formatBytes(Math.max(0, selectedSubscription.flow_total - selectedSubscription.flow_used)) }}</strong></div>
          <div><span>到期时间</span><TimeBadge :value="selectedSubscription.end_at" /></div>
        </div>

        <div v-if="subscriptionUrl" class="access-template-picker">
          <FormField
            v-slot="{ controlAttrs }"
            label="查找导出模板"
            hint="每次只返回前 25 个匹配结果；结果较多时继续缩小关键词。"
            full
          >
            <div class="inline-filter">
              <UiInput
                v-model="templateDraftQuery"
                v-bind="controlAttrs"
                placeholder="名称、标识或说明"
                @keyup.enter="searchTemplates"
              />
              <UiButton variant="secondary" size="sm" type="button" :loading="templateLoading" @click="searchTemplates">
                <UiIcon name="search" />搜索
              </UiButton>
            </div>
          </FormField>
          <FormField
            v-slot="{ controlAttrs }"
            class="access-url"
            label="订阅格式"
            :hint="deliveryHint"
            full
          >
            <UiSelect v-model="selectedTemplate" v-bind="controlAttrs" :options="templateOptions" />
          </FormField>
        </div>
        <FormField v-if="subscriptionUrl" v-slot="{ controlAttrs }" class="access-url" label="完整订阅链接" full>
          <UiInput :value="subscriptionUrl" v-bind="controlAttrs" readonly />
        </FormField>
        <PageAlert v-if="subscriptionUrl" title="用量信息仅属于当前订阅">
          Subscription-Userinfo 只返回订阅 #{{ selectedSubscription.id }} 的已用流量、总额和到期时间；其他订阅不会被合并到该链接。
        </PageAlert>
        <div class="access-actions">
          <UiButton v-if="subscriptionUrl" variant="secondary" size="sm" type="button" @click="copy">
            <UiIcon name="copy" />{{ copyLabel }}
          </UiButton>
          <UiButton variant="secondary" size="sm" type="button" :disabled="!accessReady || working" @click="ask(access.configured ? 'rotate' : 'generate')">
            {{ access.configured ? '轮换此链接' : '生成此链接' }}
          </UiButton>
          <UiButton v-if="accessReady && access.configured" variant="danger" size="sm" type="button" :disabled="working" @click="ask('revoke')">吊销此链接</UiButton>
        </div>
      </div>
      <EmptyState v-else icon="key" title="请选择有效订阅" description="每条订阅分别生成和管理自己的客户端链接。" />
    </UiSection>

    <aside class="account-notice">
      <UiIcon name="shield" />
      <div>
        <strong>每条链接都是独立访问凭证</strong>
        <p>轮换或吊销只影响当前选择的订阅，不会改变同一账户下其他订阅的链接。</p>
      </div>
    </aside>

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
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  fetchActiveSubscriptionTemplatesPage,
  fetchAccountProtocolLoads,
  fetchAccountSubscriptionsPage,
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
import { formatBytes, formatUnknownValue } from '../../utils/format'
import {
  buildSubscriptionURL,
  isBuiltInDeliveryMode,
  subscriptionDeliveryAuto,
  subscriptionDeliveryNative,
} from '../../utils/subscriptionDelivery'

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
  { label: '已到期或耗尽', value: 'expired' },
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

const {
  items: subscriptions,
  total,
  loading: listLoading,
  refreshing,
  error: listError,
  load,
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
  return selectedSubscriptionResource.data.value
    || activeSubscriptions.value.find(item => item.id === selectedSubscriptionID.value)
    || subscriptions.value.find(item => item.id === selectedSubscriptionID.value && item.status === 'active')
    || null
})

const selectedSubscriptionResource = useRemoteResource<AdminSubscriptionListItem | null>({
  initial: () => null,
  fetch: async ({ signal }) => {
    const id = selectedSubscriptionID.value
    const page = await fetchAccountSubscriptionsPage({ subscriptionId: id, status: 'active', offset: 0, limit: 1 }, { signal })
    const selected = page.items.find(item => item.id === id && item.status === 'active')
    if (!selected) throw new Error('unavailable subscription')
    return selected
  },
  errorMessage: '所选订阅不存在或已失效，请从列表重新选择。',
})

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
    const subscriptionID = selectedSubscriptionID.value
    const result = await fetchSubscriptionAccess(subscriptionID, { signal })
    if (result.subscription_id !== subscriptionID) throw new Error('subscription access response mismatch')
    return result
  },
  errorMessage: '订阅链接加载失败。',
})

const activeSubscriptions = computed(() => activeSubscriptionResource.data.value.items)
const activeSubscriptionTotal = computed(() => activeSubscriptionResource.data.value.total)
const templates = computed(() => templateResource.data.value.items)
const templateTotal = computed(() => templateResource.data.value.total)
const templateLoading = templateResource.loading
const metadataLoading = computed(() => activeSubscriptionResource.loading.value || selectedSubscriptionResource.loading.value)
const protocolLoads = protocolLoadResource.data
const access = accessResource.data
const accessLoading = accessResource.loading
const accessReady = computed(() => accessResource.loaded.value
  && !accessLoading.value
  && !accessResource.error.value
  && access.value.subscription_id === selectedSubscriptionID.value)
const loading = computed(() => listLoading.value || metadataLoading.value || accessLoading.value || templateLoading.value || protocolLoadResource.loading.value)
const error = computed(() => listError.value || selectedSubscriptionResource.error.value || activeSubscriptionResource.error.value || templateResource.error.value)
const subscriptionUrl = computed(() => accessReady.value
  ? buildSubscriptionURL(access.value.subscription_url ? `${window.location.origin}${access.value.subscription_url}` : '', selectedTemplate.value)
  : '')
const templateOptions = computed(() => [
  { label: '自动识别（推荐）', value: subscriptionDeliveryAuto },
  { label: 'ZBoard 原生格式（Base64）', value: subscriptionDeliveryNative },
  ...templates.value
    .filter(item => !isBuiltInDeliveryMode(item.slug))
    .map(item => ({ label: `${item.name} · ${item.content_type || '系统格式'}`, value: item.slug })),
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

function percent(sub: AdminSubscriptionListItem) {
  return sub.flow_total ? Math.min(100, Math.round(sub.flow_used / sub.flow_total * 100)) : 0
}

function subTone(value: string): 'success' | 'warning' | 'danger' | 'neutral' {
  return value === 'active' ? 'success' : value === 'expired' ? 'warning' : value === 'exhausted' ? 'danger' : 'neutral'
}

function subLabel(value: string) {
  return ({ active: '有效', expired: '已到期', exhausted: '流量耗尽', canceled: '已取消', inactive: '无效' } as Record<string, string>)[value]
    || formatUnknownValue('状态', value)
}

function selectDefaultSubscription() {
  if (selectedSubscriptionID.value) return
  selectedSubscriptionID.value = activeSubscriptions.value[0]?.id || 0
}

async function loadAccess() {
  if (working.value && mutationSubscriptionID === selectedSubscriptionID.value) return
  if (!selectedSubscriptionID.value) return
  const selection = selectionGeneration
  if (selectedSubscription.value) selectedSubscriptionResource.replace(selectedSubscription.value)
  else if (!await selectedSubscriptionResource.load() || disposed || selection !== selectionGeneration) return
  await accessResource.load()
}

async function loadMetadata() {
  if (await activeSubscriptionResource.load()) {
    selectDefaultSubscription()
    await loadAccess()
    if (!disposed) await syncURL(true)
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
  if (working.value || subscriptionID === selectedSubscriptionID.value) return
  selectedSubscriptionID.value = subscriptionID
  message.value = ''
  await Promise.all([syncURL(), loadAccess()])
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
  if (!selectedSubscription.value || !accessReady.value || working.value) return
  kind.value = next
  confirmOpen.value = true
}

async function execute() {
  if (!selectedSubscription.value || !accessReady.value || working.value || !confirmOpen.value) return
  const subscriptionID = selectedSubscription.value.id
  const selection = selectionGeneration
  const action = kind.value
  const isCurrent = () => !disposed && selection === selectionGeneration && subscriptionID === selectedSubscriptionID.value
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
  accessResource.reset()
  copyLabel.value = '复制链接'
  message.value = ''
  confirmOpen.value = false
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

onMounted(loadAll)
onBeforeUnmount(() => { disposed = true; selectionGeneration += 1 })
</script>

<style scoped>
.quota-cell {
  min-width: 180px;
}

.quota-cell > span,
.quota-cell small {
  display: block;
  font-size: 10px;
}

.quota-cell small {
  margin-top: 6px;
  color: var(--muted);
}

.quota-cell .usage-track {
  height: 5px;
  margin-top: 7px;
}

.subscription-row-actions {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
}

.selected-subscription-row td {
  background: var(--primary-soft);
}

.subscription-access-context {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.subscription-access-context > div {
  display: grid;
  gap: 4px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: var(--surface-soft);
}

.subscription-access-context span {
  color: var(--muted);
  font-size: 10px;
}

.subscription-access-context strong {
  font-size: 12px;
}

.access-template-picker {
  display: grid;
  gap: 12px;
}

.inline-filter {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

@media (max-width: 760px) {
  .subscription-row-actions,
  .subscription-access-context,
  .inline-filter {
    grid-template-columns: 1fr;
  }

  .subscription-row-actions {
    display: grid;
  }
}
</style>
