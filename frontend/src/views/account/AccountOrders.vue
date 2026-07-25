<template>
  <section class="account-page stack">
    <PageHeader title="我的订单" description="按状态查看商品快照、金额和处理进度。" eyebrow="ORDERS">
      <template #actions>
        <RouterLink class="button" to="/account/plans"><UiIcon name="plus" />购买套餐</RouterLink>
      </template>
    </PageHeader>

    <TransientFeedback
      :success="message"
      :error="error"
      success-title="订单已更新"
      error-title="订单操作失败"
    />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(status)" @clear="clearFilters">
          <WorkbenchFilterSelect v-model="status" label="订单状态" :options="statusOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions>
        <UiButton variant="secondary" size="sm" type="button" :disabled="loading" @click="load">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>

      <DataTable v-if="orders.length" caption="我的订单列表" :row-count="total" :min-width="820">
        <thead>
          <tr>
            <th class="table-primary-column">订单</th>
            <th>商品规格</th>
            <th>金额</th>
            <th>状态</th>
            <th data-column-priority="2">时间</th>
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in orders" :key="item.id">
            <td class="table-primary-column">
              <div class="cell-title"><strong>#{{ item.id }}</strong><span class="mono">{{ item.trade_no }}</span></div>
            </td>
            <td>
              <div class="cell-title">
                <strong>{{ item.plan_name || `套餐 #${item.plan_id}` }}</strong>
                <span>{{ item.sku_name || `SKU #${item.plan_sku_id}` }}</span>
              </div>
            </td>
            <td>{{ formatCurrency(item.amount_cents, item.currency) }}</td>
            <td><StatusBadge :tone="tone(item.status)">{{ label(item.status) }}</StatusBadge></td>
            <td data-column-priority="2"><TimeBadge :value="item.created_at" /></td>
            <td class="table-action-column">
              <UiButton
                v-if="item.status === 'pending'"
                variant="danger"
                size="sm"
                type="button"
                @click="askCancel(item)"
              >
                取消
              </UiButton>
            </td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState
        v-else
        icon="billing"
        title="没有匹配订单"
        description="从套餐页面创建第一笔订单，或调整筛选条件。"
      />

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

    <aside class="account-notice">
      <UiIcon name="clock" />
      <div>
        <strong>当前采用人工确认流程</strong>
        <p>订单创建后保持“待支付”状态，管理员确认收款后会自动生成订阅。用户端不会提供虚假的即时支付按钮。</p>
      </div>
    </aside>

    <ConfirmDialog
      :open="confirmOpen"
      title="取消这笔订单？"
      message="取消后无法继续处理，如需购买需要重新创建订单。"
      confirm-text="确认取消"
      tone="danger"
      :busy="canceling"
      @close="confirmOpen = false"
      @confirm="doCancel"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { cancelOrder, fetchAccountOrdersPage, type AdminOrderListItem } from '../../api/client'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
import DataTable from '../../components/DataTable.vue'
import DataWorkbench from '../../components/DataWorkbench.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TablePager from '../../components/TablePager.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterSelect from '../../components/WorkbenchFilterSelect.vue'
import { useRemoteTable } from '../../composables/useRemoteTable'
import { formatCurrency, formatUnknownValue } from '../../utils/format'

const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 25)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const status = ref(String(route.query.status || ''))
const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '待支付', value: 'pending' },
  { label: '已支付', value: 'paid' },
  { label: '失败', value: 'failed' },
  { label: '已取消', value: 'canceled' },
]
const message = ref('')
const confirmOpen = ref(false)
const canceling = ref(false)
const target = ref<AdminOrderListItem | null>(null)

const { items: orders, total, loading, refreshing, error, load } = useRemoteTable<AdminOrderListItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchAccountOrdersPage({
    status: status.value || undefined,
    offset: offset.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '订单加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

function tone(value: string): 'success' | 'warning' | 'danger' | 'neutral' {
  return value === 'paid' ? 'success' : value === 'pending' ? 'warning' : value === 'failed' ? 'danger' : 'neutral'
}

function label(value: string) {
  return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[value]
    || formatUnknownValue('状态', value)
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

function askCancel(item: AdminOrderListItem) {
  target.value = item
  confirmOpen.value = true
}

async function doCancel() {
  if (!target.value) return
  canceling.value = true
  message.value = ''
  try {
    await cancelOrder(target.value.id)
    confirmOpen.value = false
    target.value = null
    message.value = '订单已取消。'
    await load()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '订单取消失败。'
  } finally {
    canceling.value = false
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

onMounted(load)
</script>
