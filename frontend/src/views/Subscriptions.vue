<template>
  <section class="standard-page">
    <PageHeader title="订阅管理">
      <template #actions><PageRefreshButton label="刷新订阅" :loading="loading" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :success="quotaMessage" :error="error" success-title="订阅设置已更新" error-title="订阅数据加载失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters><WorkbenchFilterBar :active="hasFilters" :active-count="activeFilterCount" :advanced-count="advancedFilterCount" :loading="loading" @clear="clearFilters"><WorkbenchFilterInput v-model="queryFilter" label="搜索" maxlength="128" placeholder="订阅 ID、邮箱、套餐或 SKU" @apply="applyFilters" /><WorkbenchFilterSelect v-model="statusFilter" label="订阅状态" :options="statusOptions" @apply="applyFilters" /><WorkbenchFilterSelect v-model="quotaFilter" label="配额状态" :options="quotaOptions" @apply="applyFilters" /><template #advanced><WorkbenchFilterInput v-model="userFilter" label="用户 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" /><WorkbenchFilterDate v-model:from="expiresFrom" v-model:to="expiresTo" label="到期日期" preset-direction="future" @apply="applyFilters" /></template></WorkbenchFilterBar></template>
      <template #actions><UiButton as-child variant="ghost" size="sm"><RouterLink :to="trafficLink">查看流量<UiIcon name="chevron" /></RouterLink></UiButton></template>
      <TableSkeleton v-if="loading && !subscriptions.length" label="正在加载订阅" :columns="7" />
      <DataTable v-else-if="subscriptions.length" caption="订阅管理列表" :row-count="total" :min-width="1040"><thead><tr><th class="table-primary-column">订阅</th><th data-column-priority="2">用户</th><th>状态</th><th data-column-priority="2">套餐</th><th data-column-priority="1">流量配额</th><th data-column-priority="1">到期时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="sub in subscriptions" :key="sub.id"><td class="table-primary-column"><div class="cell-title"><EntityDetailButton  :data-subscription-detail-trigger="sub.id" @click="openDetail(sub.id)">{{ sub.plan_name || `订阅 #${sub.id}` }}</EntityDetailButton><TableText :value="sub.user_email || `用户 #${sub.user_id}`" /></div></td><td data-column-priority="2"><div class="cell-title cell-related"><TableText :value="sub.user_email || `用户 #${sub.user_id}`" /><span class="mono">#{{ sub.user_id }}</span></div></td><td><div class="cell-title"><StatusBadge :tone="statusTone(sub.status)">{{ statusName(sub.status) }}</StatusBadge><span v-if="sub.ended_at && sub.can_renew">可续费恢复至 <TimeBadge :value="sub.renewal_until" /></span></div></td><td data-column-priority="2"><div class="cell-title cell-related"><TableText :value="sub.plan_name || `套餐 #${sub.plan_id}`" /><TableText :value="sub.sku_name || `SKU #${sub.plan_sku_id}`" /></div></td><td data-column-priority="1"><div class="quota-cell"><span>{{ formatBytes(sub.flow_used) }} / {{ formatBytes(sub.flow_total) }}</span><div class="usage-track"><i :style="{ width: `${usagePercent(sub)}%` }"></i></div><small>{{ sub.flow_used >= sub.flow_total ? '本周期流量已用完' : `剩余 ${formatBytes(sub.flow_total - sub.flow_used)}` }}</small></div></td><td data-column-priority="1"><TimeBadge :value="sub.end_at" :tone="sub.status === 'expired' ? 'warning' : 'neutral'" /></td><td class="table-action-column"><RowActions :label="`订阅 #${sub.id} 的操作`" :trigger-key="`subscription-${sub.id}`"><UiButton variant="secondary" size="sm" type="button" :data-subscription-detail-trigger="sub.id" @click="openDetail(sub.id)">查看详情</UiButton><UiButton as-child variant="ghost" size="sm"><RouterLink :to="adminContextLink('/admin/fair-use', { subscription: String(sub.id) })">Fair Use 观测</RouterLink></UiButton><UiButton as-child variant="ghost" size="sm"><RouterLink :to="adminContextLink('/admin/traffic', { subscription_id: String(sub.id) })">流量记录</RouterLink></UiButton><UiButton as-child variant="ghost" size="sm"><RouterLink :to="adminContextLink('/admin/orders', { user_id: String(sub.user_id) })">用户订单</RouterLink></UiButton><UiButton as-child variant="ghost" size="sm"><RouterLink :to="adminContextLink('/admin/users', { user: String(sub.user_id) })">用户详情</RouterLink></UiButton></RowActions></td></tr></tbody></DataTable>
      <EmptyState v-else-if="!error" icon="plans" title="没有匹配订阅" description="调整筛选条件后重试。" />
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(detailID)" :title="selectedSubscription?.plan_name || '订阅详情'" :description="selectedSubscription ? `订阅 #${selectedSubscription.id} · ${selectedSubscription.user_email || `用户 #${selectedSubscription.user_id}`}` : '正在加载订阅权益快照'" :return-focus-selector="detailID ? `[data-row-action-trigger='subscription-${detailID}']` : ''" @close="closeDetail">
      <PageAlert v-if="detailError" tone="danger" title="订阅详情加载失败">{{ detailError }}</PageAlert>
      <div v-if="detailLoading && !selectedSubscription" class="detail-loading" role="status">正在加载订阅详情…</div>
      <SubscriptionDetail v-else-if="selectedSubscription" :key="selectedSubscription.id" ref="subscriptionDetail" :subscription="selectedSubscription" @changed="quotaSaved" />
    </DetailDrawer>
  </section>
</template>

<script setup lang="ts">
import EntityDetailButton from '../components/EntityDetailButton.vue'
import TableText from '../components/TableText.vue'
import SubscriptionDetail from '../components/SubscriptionDetail.vue'
import { useUnsavedChangesGuard } from '../composables/useFormState'
import { computed, onMounted, ref, watch } from 'vue'
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { fetchAdminSubscriptionDetail, fetchSubscriptionsPage, type AdminSubscriptionDetail, type AdminSubscriptionListItem } from '../api/client'
import DataTable from '../components/DataTable.vue'
import TableSkeleton from '../components/TableSkeleton.vue'
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
import { useRemoteResource } from '../composables/useRemoteResource'
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
const detailID = ref(0)
const quotaMessage = ref('')
const subscriptionDetail = ref<InstanceType<typeof SubscriptionDetail> | null>(null)
useUnsavedChangesGuard(() => Boolean(subscriptionDetail.value?.dirty), () => subscriptionDetail.value?.confirmLeave() ?? true)
onBeforeRouteUpdate(async (to, from) => { if (to.query.subscription !== from.query.subscription && subscriptionDetail.value && !await subscriptionDetail.value.confirmLeave()) return false })
const detailResource = useRemoteResource<AdminSubscriptionDetail | null>({
  initial: () => null,
  fetch: ({ signal }) => fetchAdminSubscriptionDetail(detailID.value, { signal }),
  errorMessage: '订阅详情加载失败。',
})
const { data: selectedSubscription, loading: detailLoading, error: detailError } = detailResource
const trafficLink = computed(() => adminContextLink('/admin/traffic', userFilter.value ? { user_id: userFilter.value } : {}))
const statusOptions = [{ label: '全部状态', value: '' }, { label: '有效', value: 'active' }, { label: '已到期', value: 'expired' }, { label: '已取消', value: 'canceled' }, { label: '已结束', value: 'ended' }]
const quotaOptions = [{ label: '全部配额', value: '' }, { label: '仍有余量', value: 'available' }, { label: '已经耗尽', value: 'exhausted' }]
const advancedFilterCount = computed(() => [userFilter.value, expiresFrom.value || expiresTo.value].filter(Boolean).length)
const activeFilterCount = computed(() => [queryFilter.value, statusFilter.value, quotaFilter.value].filter(Boolean).length + advancedFilterCount.value)
const hasFilters = computed(() => Boolean(queryFilter.value || statusFilter.value || quotaFilter.value || userFilter.value || expiresFrom.value || expiresTo.value))
const { items: subscriptions, total, loading, refreshing, error, load } = useRemoteTable<AdminSubscriptionListItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchSubscriptionsPage({ q: queryFilter.value || undefined, userId: Number(userFilter.value) || undefined, status: statusFilter.value || undefined, quota: quotaFilter.value || undefined, expiresFrom: expiresFrom.value || undefined, expiresTo: expiresTo.value || undefined, offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '订阅数据加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
async function quotaSaved() { quotaMessage.value = '流量与重置设置已保存'; await Promise.all([detailResource.load(), load()]) }
function usagePercent(sub: AdminSubscriptionListItem) { return sub.flow_total ? Math.min(100, Math.round(sub.flow_used / sub.flow_total * 100)) : 0 }
function statusName(status: string) { return ({ active: '有效', expired: '已到期', canceled: '已取消', ended: '已结束' } as Record<string, string>)[status] || formatUnknownValue('状态', status) }
function statusTone(status: string): 'success' | 'warning' | 'neutral' { return status === 'active' ? 'success' : status === 'expired' ? 'warning' : 'neutral' }
function adminContextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }
async function syncURL(replace = false) { const page = Math.floor(offset.value / limit.value) + 1; const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(queryFilter.value ? { q: queryFilter.value } : {}), ...(statusFilter.value ? { status: statusFilter.value } : {}), ...(quotaFilter.value ? { quota: quotaFilter.value } : {}), ...(userFilter.value ? { user_id: userFilter.value } : {}), ...(expiresFrom.value ? { expires_from: expiresFrom.value } : {}), ...(expiresTo.value ? { expires_to: expiresTo.value } : {}), ...(page > 1 ? { page: String(page) } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}), ...(detailID.value ? { subscription: String(detailID.value) } : {}) } }; await (replace ? router.replace(location) : router.push(location)) }
async function applyFilters() { offset.value = 0; await syncURL(); await load() }
async function clearFilters() { queryFilter.value = ''; statusFilter.value = ''; quotaFilter.value = ''; userFilter.value = ''; expiresFrom.value = ''; expiresTo.value = ''; offset.value = 0; await syncURL(); await load() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await load() }
async function openDetail(id: number) { await router.push({ query: { ...route.query, subscription: String(id) } }) }
async function closeDetail() { const { subscription: _subscription, ...query } = route.query; await router.push({ query }) }
async function syncDetailFromRoute() {
  const id = Number(route.query.subscription)
  if (!Number.isInteger(id) || id <= 0) {
    detailResource.reset(); detailID.value = 0
    return
  }
  if (detailID.value === id && (selectedSubscription.value?.id === id || detailLoading.value)) return
  detailResource.reset()
  detailID.value = id
  await detailResource.load()
}
watch(() => route.fullPath, async () => { const nextQuery = String(route.query.q || ''), nextStatus = String(route.query.status || ''), nextQuota = String(route.query.quota || ''), nextUser = String(route.query.user_id || ''), nextExpiresFrom = String(route.query.expires_from || ''), nextExpiresTo = String(route.query.expires_to || ''); const rawLimit = Number(route.query.limit), nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50, nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit; if (nextQuery !== queryFilter.value || nextStatus !== statusFilter.value || nextQuota !== quotaFilter.value || nextUser !== userFilter.value || nextExpiresFrom !== expiresFrom.value || nextExpiresTo !== expiresTo.value || nextLimit !== limit.value || nextOffset !== offset.value) { queryFilter.value = nextQuery; statusFilter.value = nextStatus; quotaFilter.value = nextQuota; userFilter.value = nextUser; expiresFrom.value = nextExpiresFrom; expiresTo.value = nextExpiresTo; limit.value = nextLimit; offset.value = nextOffset; void load() } await syncDetailFromRoute() })
onMounted(() => { void load(); void syncDetailFromRoute() })
</script>

<style scoped>
.page-alert,.subscription-metrics { margin-bottom: 16px; }.count-label { color: var(--muted); font-size: 12px; }.toolbar select { min-width: 180px; }.quota-cell { min-width: 190px; }.quota-cell > span,.quota-cell small { display: block; font-size:12px; }.quota-cell small { margin-top: 6px; color: var(--muted); }.quota-cell .usage-track { height: 5px; margin-top: 7px; }
.toolbar .p-select { min-width: 180px; }
</style>
