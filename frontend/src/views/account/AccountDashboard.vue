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

    <TransientFeedback :error="error" error-title="账户概览加载失败" />

    <UiMetricStrip :columns="3">
      <MetricCard
        label="剩余流量"
        :value="formatBytes(summary.remaining_bytes)"
        icon="activity"
        status="流量"
        tone="info"
        :meta="`累计使用 ${formatBytes(summary.total_used_bytes)}`"
      />
      <MetricCard
        label="有效订阅"
        :value="activeSubscriptionTotal"
        icon="plans"
        status="服务中"
        :tone="activeSubscriptionTotal ? 'success' : 'neutral'"
        icon-tone="success"
        meta="仅加载最近 3 条"
      />
      <MetricCard
        label="待处理订单"
        :value="pendingOrderTotal"
        icon="billing"
        status="待支付"
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
          v-else
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
      <EmptyState v-else icon="billing" title="还没有订单" description="从套餐页面创建第一笔订单。" />
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
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
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import { useAppStore } from '../../stores/app'
import { formatBytes, formatCurrency, formatUnknownValue } from '../../utils/format'

const app = useAppStore()
const loading = ref(false)
const error = ref('')
const summary = ref<Record<string, any>>({})
const activeSubscriptions = ref<AdminSubscriptionListItem[]>([])
const activeSubscriptionTotal = ref(0)
const orders = ref<AdminOrderListItem[]>([])
const recentOrderTotal = ref(0)
const pendingOrderTotal = ref(0)

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
  loading.value = true
  error.value = ''
  try {
    const [summaryResult, activePage, recentOrderPage, pendingOrderPage] = await Promise.all([
      fetchTrafficSummary(),
      fetchAccountSubscriptionsPage({ status: 'active', offset: 0, limit: 3 }),
      fetchAccountOrdersPage({ offset: 0, limit: 3 }),
      fetchAccountOrdersPage({ status: 'pending', offset: 0, limit: 1 }),
    ])
    summary.value = summaryResult
    activeSubscriptions.value = activePage.items
    activeSubscriptionTotal.value = activePage.total
    orders.value = recentOrderPage.items
    recentOrderTotal.value = recentOrderPage.total
    pendingOrderTotal.value = pendingOrderPage.total
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '账户数据加载失败。'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
