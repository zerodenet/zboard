<template>
  <section class="account-page stack">
    <PageHeader title="订阅配置" description="查看服务实例，并管理客户端使用的专属订阅链接。" eyebrow="SUBSCRIPTION">
      <template #actions>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="loadAll">
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

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
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

      <DataTable v-if="subscriptions.length" caption="我的订阅列表" :row-count="total" :min-width="820">
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
          <tr v-for="sub in subscriptions" :key="sub.id">
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
              <RouterLink class="button button-secondary button-sm" to="/account/plans">续费套餐</RouterLink>
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
          :loading="loading"
          @change="changePage"
        />
      </template>
    </DataWorkbench>

    <UiSection title="协议实时负载" description="仅显示当前有效套餐可使用的协议；活跃人数和连接数来自最近两分钟的 Zero 会话事件。">
      <template #meta><TimeBadge :value="protocolLoads.sampled_at" /></template>
      <div class="panel-body">
        <DataTable v-if="protocolLoads.items.length" caption="可用协议实时负载" :row-count="protocolLoads.items.length" :min-width="720">
          <thead><tr><th class="table-primary-column">使用位置</th><th>协议</th><th class="numeric-column">活跃用户</th><th class="numeric-column">活跃连接</th><th data-column-priority="2">最近活动</th></tr></thead>
          <tbody><tr v-for="item in protocolLoads.items" :key="item.protocol_endpoint_id"><td class="table-primary-column"><div class="cell-title"><strong>{{ item.name }}</strong><span>{{ item.region || '未设置区域' }}</span></div></td><td><StatusBadge :tone="item.active_flows ? 'success' : 'neutral'" icon="activity">{{ item.protocol.toUpperCase() }}</StatusBadge></td><td class="numeric-column">{{ item.active_users }}</td><td class="numeric-column">{{ item.active_flows }}</td><td data-column-priority="2"><TimeBadge :value="item.last_activity_at" /></td></tr></tbody>
        </DataTable>
        <EmptyState v-else icon="activity" title="暂无可用协议" description="拥有有效套餐且节点在线后，会在这里显示对应协议的实时使用人数和连接数。" />
      </div>
    </UiSection>

    <UiSection title="专属订阅链接" description="将链接添加到受支持的客户端；凭证状态与订阅列表分页无关。">
      <template #meta>
        <StatusBadge :tone="access.configured ? 'success' : 'warning'">
          {{ access.configured ? '已启用' : '未生成' }}
        </StatusBadge>
      </template>
      <div class="panel-body access-panel">
        <span class="access-icon"><UiIcon name="key" /></span>
        <div v-if="access.configured" class="access-meta">
          <strong>订阅凭证已启用</strong>
          <p>凭证前缀 <code>{{ access.token_prefix }}…</code></p>
          <div class="access-time"><span>最近使用</span><TimeBadge :value="access.last_used_at" /></div>
        </div>
        <div v-else class="access-meta">
          <strong>尚未生成订阅链接</strong>
          <p>拥有有效订阅后再生成链接，可以直接导入客户端。</p>
        </div>
        <div v-if="baseSubscriptionUrl" class="access-template-picker">
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
        <PageAlert v-if="subscriptionUrl" title="客户端可读取订阅用量">
          每次拉取都会通过 Subscription-Userinfo 响应头返回累计已用流量、总额和到期时间；支持该约定的客户端可以直接展示。模板只改变配置正文，不改变这些元信息。
        </PageAlert>
        <div class="access-actions">
          <UiButton v-if="subscriptionUrl" variant="secondary" size="sm" type="button" @click="copy">
            <UiIcon name="copy" />{{ copyLabel }}
          </UiButton>
          <UiButton
            variant="secondary"
            size="sm"
            type="button"
            :disabled="!activeSubscriptionTotal"
            @click="ask(access.configured ? 'rotate' : 'generate')"
          >
            {{ access.configured ? '轮换链接' : '生成链接' }}
          </UiButton>
          <UiButton v-if="access.configured" variant="danger" size="sm" type="button" @click="ask('revoke')">吊销</UiButton>
        </div>
        <p v-if="!activeSubscriptionTotal" class="field-hint">需要先拥有有效订阅。</p>
      </div>
    </UiSection>

    <aside class="account-notice">
      <UiIcon name="shield" />
      <div>
        <strong>链接就是访问凭证</strong>
        <p>不要发送到公开群组或截图分享。怀疑泄露时立即轮换，旧链接会马上失效。</p>
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
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  fetchActiveSubscriptionTemplatesPage,
  fetchAccountProtocolLoads,
  fetchAccountSubscriptionsPage,
  fetchSubscriptionAccess,
  revokeSubscriptionAccess,
  rotateSubscriptionAccess,
  type AccountProtocolLoadSnapshot,
  type AdminSubscriptionListItem,
} from '../../api/client'
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
const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '有效', value: 'active' },
  { label: '已到期或耗尽', value: 'expired' },
  { label: '已取消', value: 'canceled' },
]
const access = ref<any>({ configured: false })
const templates = ref<any[]>([])
const templateTotal = ref(0)
const templateQuery = ref('')
const templateDraftQuery = ref('')
const templateLoading = ref(false)
const activeSubscriptionTotal = ref(0)
const selectedTemplate = ref(subscriptionDeliveryAuto)
const baseSubscriptionUrl = ref('')
const copyLabel = ref('复制链接')
const metadataLoading = ref(false)
const metadataError = ref('')
const protocolLoads = ref<AccountProtocolLoadSnapshot>({ sampled_at: '', activity_window_seconds: 120, items: [] })
const working = ref(false)
const message = ref('')
const confirmOpen = ref(false)
const kind = ref<'generate' | 'rotate' | 'revoke'>('generate')

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

const loading = computed(() => listLoading.value || metadataLoading.value || templateLoading.value)
const error = computed({
  get: () => listError.value || metadataError.value,
  set: value => { metadataError.value = value },
})
const subscriptionUrl = computed(() => {
  return buildSubscriptionURL(baseSubscriptionUrl.value, selectedTemplate.value)
})
const templateOptions = computed(() => [
  { label: '自动识别（推荐）', value: subscriptionDeliveryAuto },
  { label: 'Zboard 原生 JSON', value: subscriptionDeliveryNative },
  ...templates.value
    .filter(item => !isBuiltInDeliveryMode(item.slug))
    .map(item => ({ label: `${item.name} · ${item.content_type || '系统格式'}`, value: item.slug })),
])
const deliveryHint = computed(() => {
  if (templateTotal.value > templates.value.length) {
    return `当前显示 ${templates.value.length} 项，共 ${templateTotal.value} 项；可通过搜索定位其他模板。`
  }
  if (selectedTemplate.value === subscriptionDeliveryAuto) {
    return '服务端按 User-Agent 自动选择 ZNet Sink、Clash/Mihomo 或 sing-box；无法识别时返回原生 JSON。'
  }
  if (selectedTemplate.value === subscriptionDeliveryNative) {
    return '始终返回 Zboard 原生 JSON，不受客户端 User-Agent 影响。'
  }
  return '显式选择优先于 User-Agent；只改变配置正文，不改变凭证、节点范围或流量计费。'
})
const confirmTitle = computed(() => kind.value === 'generate' ? '生成订阅链接？' : kind.value === 'rotate' ? '轮换订阅链接？' : '吊销订阅链接？')
const confirmText = computed(() => kind.value === 'generate' ? '生成链接' : kind.value === 'rotate' ? '确认轮换' : '确认吊销')
const confirmMessage = computed(() => kind.value === 'generate'
  ? '生成后可随时回到本页查看和复制完整链接。'
  : kind.value === 'rotate'
    ? '旧链接会立即失效，已有客户端需要更新。'
    : '吊销后所有客户端都无法继续拉取配置。')

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

async function loadMetadata() {
  metadataLoading.value = true
  metadataError.value = ''
  try {
    const [activePage, accessResult, templateResult, protocolLoadResult] = await Promise.all([
      fetchAccountSubscriptionsPage({ status: 'active', offset: 0, limit: 1 }),
      fetchSubscriptionAccess(),
      fetchActiveSubscriptionTemplatesPage({ q: templateQuery.value || undefined, offset: 0, limit: 25 }),
      fetchAccountProtocolLoads(),
    ])
    activeSubscriptionTotal.value = activePage.total
    access.value = accessResult
    templates.value = templateResult.items
    templateTotal.value = templateResult.total
    protocolLoads.value = protocolLoadResult
    baseSubscriptionUrl.value = access.value.subscription_url ? `${window.location.origin}${access.value.subscription_url}` : ''
    if (!isBuiltInDeliveryMode(selectedTemplate.value) && !templates.value.some(item => item.slug === selectedTemplate.value)) {
      selectedTemplate.value = subscriptionDeliveryAuto
    }
  } catch (cause: any) {
    metadataError.value = cause?.response?.data?.message || '订阅配置加载失败。'
  } finally {
    metadataLoading.value = false
  }
}

async function searchTemplates() {
  templateQuery.value = templateDraftQuery.value.trim()
  templateLoading.value = true
  metadataError.value = ''
  try {
    const result = await fetchActiveSubscriptionTemplatesPage({
      q: templateQuery.value || undefined,
      offset: 0,
      limit: 25,
    })
    templates.value = result.items
    templateTotal.value = result.total
    if (!isBuiltInDeliveryMode(selectedTemplate.value) && !templates.value.some(item => item.slug === selectedTemplate.value)) {
      selectedTemplate.value = subscriptionDeliveryAuto
    }
  } catch (cause: any) {
    metadataError.value = cause?.response?.data?.message || '订阅模板搜索失败。'
  } finally {
    templateLoading.value = false
  }
}

async function loadAll() {
  await Promise.all([load(), loadMetadata()])
}

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = {
    query: {
      ...(status.value ? { status: status.value } : {}),
      ...(page > 1 ? { page: String(page) } : {}),
      ...(limit.value !== 25 ? { limit: String(limit.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
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
  kind.value = next
  confirmOpen.value = true
}

async function execute() {
  working.value = true
  metadataError.value = ''
  message.value = ''
  try {
    if (kind.value === 'revoke') {
      await revokeSubscriptionAccess()
      access.value = { configured: false }
      baseSubscriptionUrl.value = ''
      message.value = '订阅链接已吊销。'
    } else {
      const result = await rotateSubscriptionAccess()
      access.value = result
      baseSubscriptionUrl.value = `${window.location.origin}${result.subscription_url}`
      copyLabel.value = '复制链接'
      message.value = kind.value === 'rotate' ? '订阅链接已轮换，可随时在本页复制新链接。' : '订阅链接已生成，可随时在本页查看。'
    }
    confirmOpen.value = false
  } catch (cause: any) {
    metadataError.value = cause?.response?.data?.message || '操作失败。'
  } finally {
    working.value = false
  }
}

async function copy() {
  try {
    await navigator.clipboard.writeText(subscriptionUrl.value)
    copyLabel.value = '已复制'
  } catch {
    copyLabel.value = '复制失败'
  }
}

watch(() => route.fullPath, async () => {
  const nextStatus = String(route.query.status || '')
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 25
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  if (nextStatus !== status.value || nextLimit !== limit.value || nextOffset !== offset.value) {
    status.value = nextStatus
    limit.value = nextLimit
    offset.value = nextOffset
    await load()
  }
})

onMounted(loadAll)
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

.access-template-picker {
  display: grid;
  gap: 12px;
}

.inline-filter {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

@media (max-width: 620px) {
  .inline-filter {
    grid-template-columns: 1fr;
  }
}
</style>
