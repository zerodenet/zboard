<template>
  <section class="standard-page">
    <PageHeader title="订阅管理" description="查看全站服务实例、配额和到期状态；个人订阅链接仍由用户在个人中心管理。" eyebrow="Service Delivery">
      <template #actions><PageRefreshButton label="刷新订阅" :loading="loading" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :error="error" error-title="订阅数据加载失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters><WorkbenchFilterBar :active="hasFilters" @clear="clearFilters"><WorkbenchFilterInput v-model="queryFilter" label="搜索" maxlength="128" placeholder="订阅 ID、邮箱、套餐或 SKU" @apply="applyFilters" /><WorkbenchFilterSelect v-model="statusFilter" label="订阅状态" :options="statusOptions" @apply="applyFilters" /><WorkbenchFilterSelect v-model="quotaFilter" label="配额状态" :options="quotaOptions" @apply="applyFilters" /><WorkbenchFilterInput v-model="userFilter" label="用户 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" /><WorkbenchFilterDate v-model:from="expiresFrom" v-model:to="expiresTo" label="到期日期" @apply="applyFilters" /></WorkbenchFilterBar></template>
      <template #actions><RouterLink class="button button-ghost button-sm" :to="trafficLink">查看流量<UiIcon name="chevron" /></RouterLink></template>
      <DataTable v-if="subscriptions.length" caption="订阅管理列表" :row-count="total" :min-width="1040"><thead><tr><th class="table-primary-column">订阅</th><th data-column-priority="2">用户</th><th>状态</th><th data-column-priority="2">套餐</th><th data-column-priority="1">流量配额</th><th data-column-priority="1">到期时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="sub in subscriptions" :key="sub.id"><td class="table-primary-column"><div class="cell-title"><strong class="mono">#{{ sub.id }}</strong><span>{{ sub.user_email || `用户 #${sub.user_id}` }}</span></div></td><td data-column-priority="2"><div class="cell-title"><strong>{{ sub.user_email || `用户 #${sub.user_id}` }}</strong><span class="mono">#{{ sub.user_id }}</span></div></td><td><StatusBadge :tone="statusTone(sub.status)">{{ statusName(sub.status) }}</StatusBadge></td><td data-column-priority="2"><div class="cell-title"><strong>{{ sub.plan_name || `套餐 #${sub.plan_id}` }}</strong><span>{{ sub.sku_name || `SKU #${sub.plan_sku_id}` }}</span></div></td><td data-column-priority="1"><div class="quota-cell"><span>{{ formatBytes(sub.flow_used) }} / {{ formatBytes(sub.flow_total) }}</span><div class="usage-track"><i :style="{ width: `${usagePercent(sub)}%` }"></i></div><small>剩余 {{ formatBytes(Math.max(0, sub.flow_total - sub.flow_used)) }}</small></div></td><td data-column-priority="1"><TimeBadge :value="sub.end_at" :tone="sub.status === 'expired' ? 'warning' : 'neutral'" /></td><td class="table-action-column"><RowActions :label="`订阅 #${sub.id} 的操作`" :trigger-key="`subscription-${sub.id}`"><UiButton variant="secondary" size="sm" type="button" :data-subscription-detail-trigger="sub.id" @click="openDetail(sub.id)">查看详情</UiButton><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/fair-use', { subscription: String(sub.id) })">Fair Use 观测</RouterLink><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/traffic', { subscription_id: String(sub.id) })">流量记录</RouterLink><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/orders', { user_id: String(sub.user_id) })">用户订单</RouterLink><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/users', { user: String(sub.user_id) })">用户详情</RouterLink></RowActions></td></tr></tbody></DataTable>
      <EmptyState v-else icon="plans" title="没有匹配订阅" description="调整状态或用户筛选条件。" />
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(detailID)" :title="selectedSubscription?.plan_name || '订阅详情'" eyebrow="Subscription" :description="selectedSubscription ? `订阅 #${selectedSubscription.id} · ${selectedSubscription.user_email || `用户 #${selectedSubscription.user_id}`}` : '正在加载订阅权益快照'" :return-focus-selector="detailID ? `[data-row-action-trigger='subscription-${detailID}']` : ''" @close="closeDetail">
      <PageAlert v-if="detailError" tone="danger" title="订阅详情加载失败">{{ detailError }}</PageAlert>
      <div v-if="detailLoading" class="detail-loading" role="status">正在加载订阅详情…</div>
      <main v-else-if="selectedSubscription" class="business-detail stack">
        <section class="detail-status-strip" aria-label="订阅状态">
          <StatusBadge :tone="statusTone(selectedSubscription.status)">{{ statusName(selectedSubscription.status) }}</StatusBadge>
          <StatusBadge tone="info">{{ trafficModeName(selectedSubscription.traffic_calc_mode) }}</StatusBadge>
          <StatusBadge :tone="selectedSubscription.active_credential_count ? 'success' : 'warning'">{{ selectedSubscription.active_credential_count }} 个有效凭证</StatusBadge>
        </section>
        <section class="detail-metrics" aria-label="订阅配额">
          <div><strong>{{ formatBytes(selectedSubscription.flow_used) }}</strong><span>已用流量</span></div>
          <div><strong>{{ formatBytes(Math.max(0, selectedSubscription.flow_total - selectedSubscription.flow_used)) }}</strong><span>剩余流量</span></div>
          <div><strong>{{ selectedSubscription.device_limit }}</strong><span>设备数</span></div>
          <div><strong>{{ selectedSubscription.speed_limit_mbps || '不限' }}</strong><span>{{ selectedSubscription.speed_limit_mbps ? 'Mbps 限速' : '速率' }}</span></div>
        </section>
        <section class="detail-facts" aria-label="订阅业务快照">
          <div><span>套餐规格</span><strong>{{ selectedSubscription.plan_name || `套餐 #${selectedSubscription.plan_id}` }} / {{ selectedSubscription.sku_name || `SKU #${selectedSubscription.plan_sku_id}` }}</strong></div>
          <div><span>节点组</span><strong class="mono">#{{ selectedSubscription.node_group_id }}</strong></div>
          <div><span>开始时间</span><TimeBadge :value="selectedSubscription.start_at" /></div>
          <div><span>到期时间</span><TimeBadge :value="selectedSubscription.end_at" :tone="selectedSubscription.status === 'expired' ? 'warning' : 'neutral'" /></div>
          <div><span>流量重置</span><strong>{{ resetPolicyName(selectedSubscription.reset_policy) }}</strong></div>
          <div><span>下次重置</span><TimeBadge :value="selectedSubscription.next_reset_at" /></div>
          <div><span>凭证总数</span><strong>{{ selectedSubscription.total_credential_count }}</strong></div>
          <div><span>更新时间</span><TimeBadge :value="selectedSubscription.updated_at" /></div>
        </section>
        <div class="detail-action-row">
          <RouterLink class="button button-secondary button-sm" :to="adminContextLink('/admin/fair-use', { subscription: String(selectedSubscription.id) })">Fair Use 观测</RouterLink>
          <RouterLink class="button button-secondary button-sm" :to="adminContextLink('/admin/traffic', { subscription_id: String(selectedSubscription.id) })">查看流量记录</RouterLink>
          <RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/orders', { user_id: String(selectedSubscription.user_id) })">查看用户订单</RouterLink>
          <RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/users', { user: String(selectedSubscription.user_id) })">查看用户详情</RouterLink>
        </div>
      </main>
    </DetailDrawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchAdminSubscriptionDetail, fetchSubscriptionsPage, type AdminSubscriptionDetail, type AdminSubscriptionListItem } from '../api/client'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import EmptyState from '../components/EmptyState.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../components/WorkbenchFilterDate.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useRemoteTable } from '../composables/useRemoteTable'
import { formatBytes, formatUnknownValue } from '../utils/format'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'

const route = useRoute()
const router = useRouter()
const queryFilter = ref(String(route.query.q || ''))
const statusFilter = ref(String(route.query.status || ''))
const quotaFilter = ref(String(route.query.quota || ''))
const userFilter = ref(String(route.query.user_id || ''))
const expiresFrom = ref(String(route.query.expires_from || ''))
const expiresTo = ref(String(route.query.expires_to || ''))
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const selectedSubscription = ref<AdminSubscriptionDetail | null>(null)
const detailID = ref(0)
const detailLoading = ref(false)
const detailError = ref('')
let detailController: AbortController | null = null
const trafficLink = computed(() => adminContextLink('/admin/traffic', userFilter.value ? { user_id: userFilter.value } : {}))
const statusOptions = [{ label: '全部状态', value: '' }, { label: '有效', value: 'active' }, { label: '已到期或耗尽', value: 'expired' }, { label: '已取消', value: 'canceled' }]
const quotaOptions = [{ label: '全部配额', value: '' }, { label: '仍有余量', value: 'available' }, { label: '已经耗尽', value: 'exhausted' }]
const hasFilters = computed(() => Boolean(queryFilter.value || statusFilter.value || quotaFilter.value || userFilter.value || expiresFrom.value || expiresTo.value))
const { items: subscriptions, total, loading, refreshing, error, load } = useRemoteTable<AdminSubscriptionListItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchSubscriptionsPage({ q: queryFilter.value || undefined, userId: Number(userFilter.value) || undefined, status: statusFilter.value || undefined, quota: quotaFilter.value || undefined, expiresFrom: expiresFrom.value || undefined, expiresTo: expiresTo.value || undefined, offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '订阅数据加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
function usagePercent(sub: AdminSubscriptionListItem) { return sub.flow_total ? Math.min(100, Math.round(sub.flow_used / sub.flow_total * 100)) : 0 }
function statusName(status: string) { return ({ active: '有效', expired: '已失效', canceled: '已取消' } as Record<string, string>)[status] || formatUnknownValue('状态', status) }
function statusTone(status: string): 'success' | 'warning' | 'neutral' { return status === 'active' ? 'success' : status === 'expired' ? 'warning' : 'neutral' }
function resetPolicyName(value: number) { return ({ 0: '跟随系统', 1: '每月 1 日', 2: '按购买日每月', 3: '每年 1 月 1 日', 4: '按购买日每年', 5: '不重置' } as Record<number, string>)[value] || formatUnknownValue('重置策略', value) }
function trafficModeName(value: number) { return ({ 0: '上行 + 下行', 1: '仅上行', 2: '仅下行' } as Record<number, string>)[value] || formatUnknownValue('流量口径', value) }
function adminContextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }
async function syncURL(replace = false) { const page = Math.floor(offset.value / limit.value) + 1; const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(queryFilter.value ? { q: queryFilter.value } : {}), ...(statusFilter.value ? { status: statusFilter.value } : {}), ...(quotaFilter.value ? { quota: quotaFilter.value } : {}), ...(userFilter.value ? { user_id: userFilter.value } : {}), ...(expiresFrom.value ? { expires_from: expiresFrom.value } : {}), ...(expiresTo.value ? { expires_to: expiresTo.value } : {}), ...(page > 1 ? { page: String(page) } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}), ...(detailID.value ? { subscription: String(detailID.value) } : {}) } }; await (replace ? router.replace(location) : router.push(location)) }
async function applyFilters() { offset.value = 0; await syncURL(); await load() }
async function clearFilters() { queryFilter.value = ''; statusFilter.value = ''; quotaFilter.value = ''; userFilter.value = ''; expiresFrom.value = ''; expiresTo.value = ''; offset.value = 0; await syncURL(); await load() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await load() }
async function openDetail(id: number) { await router.push({ query: { ...route.query, subscription: String(id) } }) }
async function closeDetail() { detailController?.abort(); detailID.value = 0; selectedSubscription.value = null; detailError.value = ''; const { subscription: _subscription, ...query } = route.query; await router.push({ query }) }
async function syncDetailFromRoute() {
  const id = Number(route.query.subscription)
  if (!Number.isInteger(id) || id <= 0) {
    detailController?.abort(); detailID.value = 0; selectedSubscription.value = null; detailError.value = ''; detailLoading.value = false
    return
  }
  if (detailID.value === id && (selectedSubscription.value?.id === id || detailLoading.value)) return
  detailController?.abort()
  detailController = new AbortController()
  detailID.value = id; selectedSubscription.value = null; detailError.value = ''; detailLoading.value = true
  try { selectedSubscription.value = await fetchAdminSubscriptionDetail(id, { signal: detailController.signal }) }
  catch (cause: any) { if (cause?.name !== 'CanceledError' && cause?.name !== 'AbortError') detailError.value = cause?.response?.data?.message || '订阅详情加载失败。' }
  finally { if (detailID.value === id) detailLoading.value = false }
}
watch(() => route.fullPath, async () => { const nextQuery = String(route.query.q || ''), nextStatus = String(route.query.status || ''), nextQuota = String(route.query.quota || ''), nextUser = String(route.query.user_id || ''), nextExpiresFrom = String(route.query.expires_from || ''), nextExpiresTo = String(route.query.expires_to || ''); const rawLimit = Number(route.query.limit), nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50, nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit; if (nextQuery !== queryFilter.value || nextStatus !== statusFilter.value || nextQuota !== quotaFilter.value || nextUser !== userFilter.value || nextExpiresFrom !== expiresFrom.value || nextExpiresTo !== expiresTo.value || nextLimit !== limit.value || nextOffset !== offset.value) { queryFilter.value = nextQuery; statusFilter.value = nextStatus; quotaFilter.value = nextQuota; userFilter.value = nextUser; expiresFrom.value = nextExpiresFrom; expiresTo.value = nextExpiresTo; limit.value = nextLimit; offset.value = nextOffset; await load() } await syncDetailFromRoute() })
onMounted(async () => { await load(); await syncDetailFromRoute() })
</script>

<style scoped>
.page-alert,.subscription-metrics { margin-bottom: 16px; }.count-label { color: var(--muted); font-size: 12px; }.toolbar select { min-width: 180px; }.quota-cell { min-width: 190px; }.quota-cell > span,.quota-cell small { display: block; font-size: 10px; }.quota-cell small { margin-top: 6px; color: var(--muted); }.quota-cell .usage-track { height: 5px; margin-top: 7px; }
.toolbar .p-select { min-width: 180px; }
</style>
