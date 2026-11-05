<template>
  <section class="account-page stack account-order-page">
    <PageHeader title="我的订单">
      <template #actions>
        <UiButton as-child><RouterLink  to="/account/plans"><UiIcon name="plus" />购买套餐</RouterLink></UiButton>
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
        <PageRefreshButton label="刷新订单" :loading="loading" @click="load" />
      </template>

      <TableSkeleton v-if="loading && !orders.length" label="正在加载我的订单" :columns="6" />
      <AccountOrderList v-else-if="orders.length" :orders="orders"><template #actions="{ order }">
        <UiButton v-if="['pending', 'failed'].includes(order.status)" as-child variant="secondary" size="sm"><RouterLink :to="`/account/orders/${order.id}`">继续支付</RouterLink></UiButton>
        <UiButton v-if="order.status === 'pending'" variant="ghost" size="sm" type="button" @click="askCancel(order)">取消订单</UiButton>
      </template></AccountOrderList>
      <EmptyState
        v-else-if="!loading && !error"
        icon="billing"
        title="没有匹配订单"
        description=""
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
import AccountOrderList from '../../components/account/AccountOrderList.vue'
import PageRefreshButton from '../../components/PageRefreshButton.vue'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
import TableSkeleton from '../../components/TableSkeleton.vue'
import DataWorkbench from '../../components/DataWorkbench.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageHeader from '../../components/PageHeader.vue'
import TablePager from '../../components/TablePager.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterSelect from '../../components/WorkbenchFilterSelect.vue'
import { useRemoteTable } from '../../composables/useRemoteTable'

const route = useRoute()
const router = useRouter()
// Existing plugin return URLs remain valid, but payment belongs to one order.
watch(() => route.query.payment_order, order => {
  const id = Number(order)
  if (!Number.isSafeInteger(id) || id <= 0) return
  void router.replace({ path: `/account/orders/${id}`, query: {
    ...(typeof route.query.payment_plugin === 'string' ? { payment_plugin: route.query.payment_plugin } : {}),
  } })
}, { immediate: true })
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

<style scoped>
@media (max-width: 720px) {
  .account-order-page :deep(.workbench-toolbar) { flex-direction: row; align-items: center; flex-wrap: wrap; }
  .account-order-page :deep(.workbench-filters) { flex: 1 1 160px; width: auto; min-width: 0; }
  .account-order-page :deep(.workbench-actions) { flex: 0 0 auto; justify-content: flex-end; }
}
</style>
