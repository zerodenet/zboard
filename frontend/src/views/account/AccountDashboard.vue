<template>
  <section class="account-page stack">
    <PageHeader
      title="我的概览"
    >
      <template #actions>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="load">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <RemoteReadStatus label="剩余流量" :phase="quotaResource.phase.value" :error="quotaError" :last-success-at="quotaResource.lastSuccessAt.value" @retry="quotaResource.load()" />
    <PageAlert v-if="pendingError" tone="danger" title="待处理订单统计加载失败">{{ pendingError }} <UiButton variant="secondary" @click="pendingResource.load()">重试订单统计</UiButton></PageAlert>

    <UiMetricStrip :columns="3">
      <MetricCard
        label="剩余流量"
        :value="quotaLoaded ? formatBytes(quota.remaining_bytes) : '—'"
        icon="activity"
        :loading="!quotaLoaded && quotaLoading"
        :tone="quotaError ? 'danger' : 'neutral'"
        :meta="quotaError ? (quotaLoaded ? '上次成功读取的可用流量' : '尚未取得额度') : ''"
      />
      <MetricCard
        label="有效订阅"
        :value="activeLoaded ? activeSubscriptionTotal : '—'"
        icon="plans"
        :loading="!activeLoaded && activeLoading"
        :tone="activeError ? 'danger' : 'neutral'"

      />
      <MetricCard
        label="待处理订单"
        :value="pendingLoaded ? pendingOrderTotal : '—'"
        icon="billing"
        :loading="!pendingLoaded && pendingLoading"
        :tone="pendingError ? 'danger' : 'neutral'"

      />
    </UiMetricStrip>

    <PluginSlot name="account.overview.cards" surface="account" />

    <div class="section-grid account-overview-grid">
      <UiSection class="span-8" title="当前订阅">
        <template #actions>
          <UiButton as-child variant="ghost" size="sm"><RouterLink  to="/account/subscription">
            管理订阅<UiIcon name="chevron" />
          </RouterLink></UiButton>
        </template>
        <PageAlert v-if="activeError" tone="danger" title="有效订阅加载失败">{{ activeError }} <UiButton variant="secondary" @click="activeResource.load()">重试订阅</UiButton></PageAlert>
        <TableSkeleton v-if="activeLoading && !activeLoaded" label="正在加载有效订阅" :columns="3" :rows="3" />
        <DataTable v-if="activeSubscriptions.length" caption="最近三条有效订阅" :row-count="activeSubscriptionTotal" :min-width="620">
          <thead>
            <tr>
              <th class="table-primary-column">订阅</th>
              <th class="numeric-column">剩余流量</th>
              <th data-column-priority="1">到期时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="sub in activeSubscriptions" :key="sub.id">
              <td class="table-primary-column"><div class="cell-related">
                <RouterLink v-overflow-title="sub.plan_name" :to="{ path: '/account/subscription', query: { subscription: String(sub.id) } }">{{ sub.plan_name || `订阅 #${sub.id}` }}</RouterLink>
                <TableText v-if="sub.sku_name" :value="sub.sku_name" />
              </div></td>
              <td class="value-cell numeric-column"><strong>{{ formatBytes(remaining(sub)) }}</strong><div v-if="sub.flow_used >= sub.flow_total"><small>本周期流量已用完</small><UiButton v-if="sub.can_reset" as-child variant="ghost" size="sm"><RouterLink  :to="`/account/plans?operation=reset&subscription=${sub.id}`">购买流量重置</RouterLink></UiButton></div></td>
              <td data-column-priority="1"><TimeBadge :value="sub.end_at" mode="relative" /></td>
            </tr>
          </tbody>
        </DataTable>
        <EmptyState
          v-else-if="activeLoaded && !activeLoading && !activeError"
          icon="plans"
          title="还没有有效订阅"
          description="选择一个套餐并创建订单，订单确认后即可获得订阅服务。"
        >
          <template #actions><UiButton as-child size="sm"><RouterLink  to="/account/plans">选择套餐</RouterLink></UiButton></template>
        </EmptyState>
      </UiSection>

      <UiSection class="span-4" title="快捷操作">
        <div class="panel-body quick-links">
          <RouterLink to="/account/plans"><span><UiIcon name="plans" /></span><div><strong>购买套餐</strong></div><UiIcon name="chevron" /></RouterLink>
          <RouterLink to="/account/subscription"><span><UiIcon name="key" /></span><div><strong>订阅配置</strong></div><UiIcon name="chevron" /></RouterLink>
          <RouterLink to="/account/traffic"><span><UiIcon name="activity" /></span><div><strong>流量明细</strong></div><UiIcon name="chevron" /></RouterLink>
        </div>
      </UiSection>
    </div>

    <UiSection title="最近订单">
      <template #actions>
        <UiButton as-child variant="ghost" size="sm"><RouterLink  to="/account/orders">
          全部订单<UiIcon name="chevron" />
        </RouterLink></UiButton>
      </template>
      <PageAlert v-if="ordersError" tone="danger" title="最近订单加载失败">{{ ordersError }} <UiButton variant="secondary" @click="ordersResource.load()">重试最近订单</UiButton></PageAlert>
      <TableSkeleton v-if="ordersLoading && !ordersLoaded" label="正在加载最近订单" :columns="4" :rows="3" />
      <DataTable v-if="orders.length" caption="最近三笔订单" :row-count="recentOrderTotal" :min-width="680">
        <thead>
          <tr>
            <th class="table-primary-column">订单</th>
            <th>商品</th>
            <th class="numeric-column">金额</th>
            <th>状态</th>
            <th data-column-priority="2">创建时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="order in orders" :key="order.id">
            <td class="table-primary-column"><RouterLink :to="`/account/orders/${order.id}`">#{{ order.id }}</RouterLink></td>
            <td>
              <div class="cell-title cell-related">
                <TableText :value="order.plan_name || `套餐 #${order.plan_id}`" />
                <TableText :value="order.sku_name || `SKU #${order.plan_sku_id}`" />
              </div>
            </td>
            <td class="value-cell numeric-column">{{ formatCurrency(order.amount_cents, order.currency) }}</td>
            <td><StatusBadge :tone="orderTone(order.status)">{{ orderLabel(order.status) }}</StatusBadge></td>
            <td data-column-priority="2"><TimeBadge :value="order.created_at" /></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else-if="ordersLoaded && !ordersLoading && !ordersError" icon="billing" title="还没有订单" description="从套餐页面创建第一笔订单。" />
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { vOverflowTitle } from '../../directives/overflowTitle'
import TableSkeleton from '../../components/TableSkeleton.vue'
import PluginSlot from '../../plugins/PluginSlot.vue'
import TableText from '../../components/TableText.vue'
import { computed, onMounted } from 'vue'
import {
  fetchAccountOrdersPage,
  fetchAccountSubscriptionsPage,
  fetchTrafficQuota,
  type TrafficQuotaSnapshot,
  type AdminOrderListItem,
  type AdminSubscriptionListItem,
} from '../../api/client'
import DataTable from '../../components/DataTable.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import PageAlert from '../../components/PageAlert.vue'
import RemoteReadStatus from '../../components/RemoteReadStatus.vue'
import MetricCard from '../../components/MetricCard.vue'
import UiMetricStrip from '../../components/UiMetricStrip.vue'
import UiSection from '../../components/UiSection.vue'
import { useRemoteResource } from '../../composables/useRemoteResource'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import { useAppStore } from '../../stores/app'
import { formatBytes, formatCurrency, formatUnknownValue } from '../../utils/format'

const app = useAppStore()
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
const recentOrderTotal = computed(() => ordersResource.data.value.total)
const loading = computed(() => quotaLoading.value || activeLoading.value || ordersLoading.value || pendingLoading.value)
function remaining(sub: AdminSubscriptionListItem) {
  return Math.max(0, (sub.flow_total || 0) - (sub.flow_used || 0))
}

function orderTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  return status === 'paid' ? 'success' : status === 'pending' ? 'warning' : status === 'failed' ? 'danger' : 'neutral'
}

function orderLabel(status: string) {
  return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[status]
    || formatUnknownValue('状态', status)
}

async function load() {
  await Promise.all([quotaResource.load(), activeResource.load(), ordersResource.load(), pendingResource.load()])
}

onMounted(load)
</script>

<style scoped>
.account-overview-grid { align-items: start; }
</style>
