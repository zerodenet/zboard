<template>
  <section class="account-page stack">
    <PageHeader
      :title="`你好，${app.user.email}`"
      description="这是你的订阅、流量和订单状态总览。"
      eyebrow="MY ACCOUNT"
    >
      <template #actions>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="load">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <PageAlert v-if="summaryError" tone="danger" title="流量汇总加载失败">{{ summaryError }} <UiButton variant="secondary" @click="summaryResource.load()">重试流量汇总</UiButton></PageAlert>
    <PageAlert v-if="pendingError" tone="danger" title="待处理订单统计加载失败">{{ pendingError }} <UiButton variant="secondary" @click="pendingResource.load()">重试订单统计</UiButton></PageAlert>

    <UiMetricStrip :columns="3">
      <MetricCard
        label="剩余流量"
        :value="summaryLoaded && !summaryError ? formatBytes(summary.remaining_bytes) : '—'"
        icon="activity"
        :status="summaryLoading ? '加载中' : summaryError ? '读取失败' : '流量'"
        tone="info"
        :meta="summaryLoaded && !summaryError ? `累计使用 ${formatBytes(summary.total_used_bytes)}` : '尚未取得汇总'"
      />
      <MetricCard
        label="有效订阅"
        :value="activeLoaded && !activeError ? activeSubscriptionTotal : '—'"
        icon="plans"
        :status="activeLoading ? '加载中' : activeError ? '读取失败' : '服务中'"
        :tone="activeSubscriptionTotal ? 'success' : 'neutral'"
        icon-tone="success"
        meta="仅加载最近 3 条"
      />
      <MetricCard
        label="待处理订单"
        :value="pendingLoaded && !pendingError ? pendingOrderTotal : '—'"
        icon="billing"
        :status="pendingLoading ? '加载中' : pendingError ? '读取失败' : '待支付'"
        :tone="pendingOrderTotal ? 'warning' : 'neutral'"
        icon-tone="warning"
        meta="按当前用户完整统计"
      />
    </UiMetricStrip>

    <div class="section-grid">
      <UiSection class="span-8" title="当前订阅" description="最近三条有效服务及其配额和到期状态。">
        <template #actions>
          <RouterLink class="button button-ghost button-sm" to="/account/subscription">
            管理订阅<UiIcon name="chevron" />
          </RouterLink>
        </template>
        <PageAlert v-if="activeError" tone="danger" title="有效订阅加载失败">{{ activeError }} <UiButton variant="secondary" @click="activeResource.load()">重试订阅</UiButton></PageAlert>
        <p v-if="activeLoading" role="status">正在加载有效订阅…</p>
        <DataTable v-if="activeSubscriptions.length" caption="最近三条有效订阅" :row-count="activeSubscriptionTotal" :min-width="620">
          <thead>
            <tr>
              <th class="table-primary-column">订阅</th>
              <th>套餐</th>
              <th>剩余流量</th>
              <th data-column-priority="1">到期时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="sub in activeSubscriptions" :key="sub.id">
              <td class="table-primary-column"><strong class="mono">#{{ sub.id }}</strong></td>
              <td>
                <div class="cell-title">
                  <strong>{{ sub.plan_name || `套餐 #${sub.plan_id}` }}</strong>
                  <span>{{ sub.sku_name || `SKU #${sub.plan_sku_id}` }}</span>
                </div>
              </td>
              <td><strong>{{ formatBytes(remaining(sub)) }}</strong></td>
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
          <template #actions><RouterLink class="button button-sm" to="/account/plans">选择套餐</RouterLink></template>
        </EmptyState>
      </UiSection>

      <UiSection class="span-4" title="快捷操作" description="常用入口集中在这里。">
        <div class="panel-body quick-links">
          <RouterLink to="/account/plans"><span><UiIcon name="plans" /></span><div><strong>购买套餐</strong><small>浏览可售规格</small></div><UiIcon name="chevron" /></RouterLink>
          <RouterLink to="/account/subscription"><span><UiIcon name="key" /></span><div><strong>订阅配置</strong><small>生成或复制链接</small></div><UiIcon name="chevron" /></RouterLink>
          <RouterLink to="/account/traffic"><span><UiIcon name="activity" /></span><div><strong>流量明细</strong><small>查看每条使用记录</small></div><UiIcon name="chevron" /></RouterLink>
        </div>
      </UiSection>
    </div>

    <UiSection title="最近订单" description="最近三笔订单的处理状态。">
      <template #actions>
        <RouterLink class="button button-ghost button-sm" to="/account/orders">
          全部订单<UiIcon name="chevron" />
        </RouterLink>
      </template>
      <PageAlert v-if="ordersError" tone="danger" title="最近订单加载失败">{{ ordersError }} <UiButton variant="secondary" @click="ordersResource.load()">重试最近订单</UiButton></PageAlert>
      <p v-if="ordersLoading" role="status">正在加载最近订单…</p>
      <DataTable v-if="orders.length" caption="最近三笔订单" :row-count="recentOrderTotal" :min-width="680">
        <thead>
          <tr>
            <th class="table-primary-column">订单</th>
            <th>商品</th>
            <th>金额</th>
            <th>状态</th>
            <th data-column-priority="2">创建时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="order in orders" :key="order.id">
            <td class="table-primary-column">#{{ order.id }}</td>
            <td>
              <div class="cell-title">
                <strong>{{ order.plan_name || `套餐 #${order.plan_id}` }}</strong>
                <span>{{ order.sku_name || `SKU #${order.plan_sku_id}` }}</span>
              </div>
            </td>
            <td>{{ formatCurrency(order.amount_cents, order.currency) }}</td>
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
import { computed, onMounted } from 'vue'
import {
  fetchAccountOrdersPage,
  fetchAccountSubscriptionsPage,
  fetchTrafficSummary,
  type AdminOrderListItem,
  type AdminSubscriptionListItem,
} from '../../api/client'
import DataTable from '../../components/DataTable.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import PageAlert from '../../components/PageAlert.vue'
import MetricCard from '../../components/MetricCard.vue'
import UiMetricStrip from '../../components/UiMetricStrip.vue'
import UiSection from '../../components/UiSection.vue'
import { useRemoteResource } from '../../composables/useRemoteResource'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import { useAppStore } from '../../stores/app'
import { formatBytes, formatCurrency, formatUnknownValue } from '../../utils/format'

const app = useAppStore()
const summaryResource = useRemoteResource<Record<string, any>>({
  initial: () => ({}), fetch: ({ signal }) => fetchTrafficSummary(false, { signal }), errorMessage: '流量汇总加载失败。',
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
const { data: summary, loading: summaryLoading, error: summaryError, loaded: summaryLoaded } = summaryResource
const { loading: activeLoading, error: activeError, loaded: activeLoaded } = activeResource
const { loading: ordersLoading, error: ordersError, loaded: ordersLoaded } = ordersResource
const { data: pendingOrderTotal, loading: pendingLoading, error: pendingError, loaded: pendingLoaded } = pendingResource
const activeSubscriptions = computed(() => activeResource.data.value.items)
const activeSubscriptionTotal = computed(() => activeResource.data.value.total)
const orders = computed(() => ordersResource.data.value.items)
const recentOrderTotal = computed(() => ordersResource.data.value.total)
const loading = computed(() => summaryLoading.value || activeLoading.value || ordersLoading.value || pendingLoading.value)
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
  await Promise.all([summaryResource.load(), activeResource.load(), ordersResource.load(), pendingResource.load()])
}

onMounted(load)
</script>
