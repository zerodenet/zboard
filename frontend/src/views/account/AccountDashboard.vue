<template>
  <section class="account-page account-overview stack">
    <PageHeader title="概览">
      <template #actions>
        <PageRefreshButton label="刷新概览" :loading="loading" @click="load" />
        <UiButton as-child variant="secondary"><RouterLink to="/account/plans"><UiIcon name="plus" />购买套餐</RouterLink></UiButton>
      </template>
    </PageHeader>

    <RemoteReadStatus v-if="quotaLoaded || quotaError" label="剩余流量" :phase="quotaResource.phase.value" :error="quotaError" :last-success-at="quotaResource.lastSuccessAt.value" @retry="quotaResource.load()" />
    <PageAlert v-if="pendingError" tone="danger" title="待处理订单统计加载失败">{{ pendingError }} <UiButton variant="secondary" @click="pendingResource.load()">重试订单统计</UiButton></PageAlert>

    <dl class="account-overview-summary" aria-label="账户概况">
      <div :aria-busy="quotaLoading || undefined"><dt>剩余流量</dt><dd data-overview-quota><Skeleton v-if="!quotaLoaded && quotaLoading" class="summary-skeleton" /><template v-else>{{ quotaLoaded ? formatBytes(quota.remaining_bytes) : '—' }}</template></dd></div>
      <div :aria-busy="activeLoading || undefined"><dt>有效订阅</dt><dd data-overview-active><Skeleton v-if="!activeLoaded && activeLoading" class="summary-skeleton" /><template v-else>{{ activeLoaded ? activeSubscriptionTotal : '—' }}</template></dd></div>
      <div :aria-busy="pendingLoading || undefined"><dt>待支付订单</dt><dd data-overview-pending><Skeleton v-if="!pendingLoaded && pendingLoading" class="summary-skeleton" /><RouterLink v-else-if="pendingLoaded && pendingOrderTotal" to="/account/orders?status=pending">{{ pendingOrderTotal }}<UiIcon name="chevron" /></RouterLink><template v-else>{{ pendingLoaded ? 0 : '—' }}</template></dd></div>
    </dl>

    <PluginSlot name="account.overview.cards" surface="account" />

    <section class="overview-section" aria-labelledby="overview-subscriptions">
      <header class="overview-section-header"><h2 id="overview-subscriptions">当前套餐</h2><UiButton as-child variant="ghost" size="sm"><RouterLink to="/account/subscription">全部订阅<UiIcon name="chevron" /></RouterLink></UiButton></header>
      <PageAlert v-if="activeError" tone="danger" title="有效订阅加载失败">{{ activeError }} <UiButton variant="secondary" @click="activeResource.load()">重试订阅</UiButton></PageAlert>
      <div v-if="activeLoading && !activeLoaded" class="service-card service-card-skeleton" role="status" aria-label="正在加载有效订阅"><Skeleton class="skeleton-heading" /><Skeleton class="skeleton-value" /><Skeleton class="skeleton-line" /><Skeleton class="skeleton-action" /></div>
      <div v-if="activeSubscriptions.length" class="account-service-grid">
        <AccountSubscriptionCard v-for="(sub, index) in activeSubscriptions" :key="sub.id" :subscription="sub" :primary="index === 0" />
      </div>
      <EmptyState v-else-if="activeLoaded && !activeLoading && !activeError" icon="plans" title="还没有有效订阅" description=""><template #actions><UiButton as-child size="sm"><RouterLink to="/account/plans">选择套餐</RouterLink></UiButton></template></EmptyState>
    </section>

    <section class="overview-section" aria-labelledby="overview-orders">
      <header class="overview-section-header"><h2 id="overview-orders">最近订单</h2><UiButton as-child variant="ghost" size="sm"><RouterLink to="/account/orders">全部订单<UiIcon name="chevron" /></RouterLink></UiButton></header>
      <PageAlert v-if="ordersError" tone="danger" title="最近订单加载失败">{{ ordersError }} <UiButton variant="secondary" @click="ordersResource.load()">重试最近订单</UiButton></PageAlert>
      <div v-if="ordersLoading && !ordersLoaded" class="recent-orders-skeleton" role="status" aria-label="正在加载最近订单"><Skeleton v-for="row in 3" :key="row" class="skeleton-order" /></div>
      <AccountOrderList v-if="orders.length" :orders="orders" />
      <EmptyState v-else-if="ordersLoaded && !ordersLoading && !ordersError" icon="billing" title="还没有订单" description="" />
    </section>
  </section>
</template>

<script setup lang="ts">
import AccountOrderList from '../../components/account/AccountOrderList.vue'
import AccountSubscriptionCard from '../../components/account/AccountSubscriptionCard.vue'
import PageRefreshButton from '../../components/PageRefreshButton.vue'
import { Skeleton } from '../../components/ui/skeleton'
import PluginSlot from '../../plugins/PluginSlot.vue'
import { computed, onMounted } from 'vue'
import {
  fetchAccountOrdersPage,
  fetchAccountSubscriptionsPage,
  fetchTrafficQuota,
  type TrafficQuotaSnapshot,
  type AdminOrderListItem,
  type AdminSubscriptionListItem,
} from '../../api/client'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import PageAlert from '../../components/PageAlert.vue'
import RemoteReadStatus from '../../components/RemoteReadStatus.vue'
import { useRemoteResource } from '../../composables/useRemoteResource'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import { formatBytes } from '../../utils/format'

const quotaResource = useRemoteResource<TrafficQuotaSnapshot>({
  initial: () => ({ remaining_bytes: 0, scope_user: 0, as_of: '' }), fetch: ({ signal }) => fetchTrafficQuota(false, { signal }), errorMessage: '剩余流量加载失败。',
})
const activeResource = useRemoteResource({
  initial: () => ({ items: [] as AdminSubscriptionListItem[], total: 0 }),
  fetch: ({ signal }) => fetchAccountSubscriptionsPage({ status: 'active', offset: 0, limit: 3 }, { signal }),
  errorMessage: '有效订阅加载失败。',
})
const ordersResource = useRemoteResource({
  initial: () => ({ items: [] as AdminOrderListItem[], total: 0 }),
  fetch: ({ signal }) => fetchAccountOrdersPage({ offset: 0, limit: 3 }, { signal }), errorMessage: '最近订单加载失败。',
})
const pendingResource = useRemoteResource({
  initial: () => 0,
  fetch: async ({ signal }) => (await fetchAccountOrdersPage({ status: 'pending', offset: 0, limit: 1 }, { signal })).total,
  errorMessage: '待处理订单统计加载失败。',
})
const { data: quota, loading: quotaLoading, error: quotaError, loaded: quotaLoaded } = quotaResource
const { loading: activeLoading, error: activeError, loaded: activeLoaded } = activeResource
const { loading: ordersLoading, error: ordersError, loaded: ordersLoaded } = ordersResource
const { data: pendingOrderTotal, loading: pendingLoading, error: pendingError, loaded: pendingLoaded } = pendingResource
const activeSubscriptions = computed(() => activeResource.data.value.items)
const activeSubscriptionTotal = computed(() => activeResource.data.value.total)
const orders = computed(() => ordersResource.data.value.items)
const loading = computed(() => quotaLoading.value || activeLoading.value || ordersLoading.value || pendingLoading.value)
async function load() {
  await Promise.all([quotaResource.load(), activeResource.load(), ordersResource.load(), pendingResource.load()])
}

onMounted(load)
</script>

<style scoped>
.account-overview { gap: 28px; }
.account-overview-summary { border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--card); padding: 20px; display: grid; grid-template-columns: 2fr 1fr 1fr; gap: 24px; margin: 0; }
.account-overview-summary > div { min-width: 0; }
.account-overview-summary dt { margin-bottom: 8px; color: var(--muted-foreground); font-size: 12px; }
.account-overview-summary dd { min-height: 34px; margin: 0; font-size: 20px; line-height: 1.3; font-weight: 500; letter-spacing: -.03em; font-variant-numeric: tabular-nums; }
.account-overview-summary > div:first-child dd { font-size: 30px; font-weight: 600; }
.account-overview-summary dd a { display: inline-flex; align-items: center; gap: 8px; color: inherit; text-decoration: none; }
.account-overview-summary dd a .ui-icon { width: 14px; color: var(--muted-foreground); }
.summary-skeleton { width: 70px; height: 30px; }
.overview-section { min-width: 0; }
.overview-section-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.overview-section-header h2 { margin: 0; font-size: 14px; font-weight: 600; }
.service-card-skeleton { min-height: 196px; display: grid; gap: 20px; padding: 22px 24px; border: 1px solid var(--border); border-radius: var(--radius-md); }
.service-card-skeleton { gap: 20px; }
.skeleton-heading { width: 140px; height: 24px; }
.skeleton-value { width: 100px; height: 48px; }
.skeleton-line { height: 5px; }
.skeleton-action { width: 110px; height: 32px; }
.recent-orders-skeleton { display: grid; gap: 1px; }
.skeleton-order { height: 56px; }
.account-overview-summary a:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; border-radius: 4px; }
@media (max-width: 600px) {
  .account-overview { gap: 24px; }
  .account-overview-summary { grid-template-columns: 1.5fr 1fr 1fr; gap: 12px; padding: 16px; }
  .account-overview-summary dd { min-height: 26px; font-size: 20px; }
  .account-overview-summary > div:first-child dd { font-size: 26px; }

}
</style>
