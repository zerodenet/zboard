<template>
  <section class="account-page stack">
    <PageHeader
      title="选择套餐"
      description="先从有界目录选择套餐，再查看当前套餐的可购买规格；筛选和分页都可以通过地址恢复。"
      eyebrow="PLANS"
    >
      <template #actions>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="refreshAll">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <TransientFeedback
      :success="message"
      :error="error"
      success-title="订单已创建"
      error-title="套餐操作失败"
    />

    <DataWorkbench :total="planTotal" :loading="planLoading" :refreshing="planRefreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(planQuery || planDraftQuery)" @clear="clearPlanFilters">
          <WorkbenchFilterInput v-model="planDraftQuery" label="搜索" placeholder="名称、标识或简介" @apply="applyPlanFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable v-if="plans.length" caption="可购买套餐目录" :row-count="planTotal" :min-width="760" selectable>
        <thead>
          <tr>
            <th class="table-primary-column">套餐</th>
            <th>可售规格</th>
            <th data-column-priority="2">起售价</th>
            <th data-column-priority="1">更新时间</th>
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="plan in plans" :key="plan.id" :aria-selected="selectedPlanID === plan.id">
            <td class="table-primary-column">
              <div class="cell-title">
                <strong>{{ plan.name }}</strong>
                <span>{{ plan.slug }} · {{ plan.summary || '暂无简介' }}</span>
              </div>
            </td>
            <td><strong class="numeric-value">{{ plan.active_sku_count }}</strong></td>
            <td data-column-priority="2">
              {{ plan.primary_sku ? formatCurrency(plan.primary_sku.price_cents, plan.primary_sku.currency) : '暂无' }}
            </td>
            <td data-column-priority="1"><TimeBadge :value="plan.updated_at" /></td>
            <td class="table-action-column">
              <UiButton size="sm" :variant="selectedPlanID === plan.id ? 'primary' : 'secondary'" type="button" @click="selectPlan(plan)">
                {{ selectedPlanID === plan.id ? '已选择' : '查看规格' }}
              </UiButton>
            </td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else icon="plans" :title="planQuery ? '没有匹配的套餐' : '暂无可售套餐'" description="可以调整关键词后重试。" />

      <template #footer>
        <TablePager
          :total="planTotal"
          :offset="planOffset"
          :limit="planLimit"
          :loading="planLoading"
          @change="changePlanPage"
        />
      </template>
    </DataWorkbench>

    <UiSection
      v-if="selectedPlanID"
      :title="selectedPlan?.name || `套餐 #${selectedPlanID}`"
      description="这里只加载当前套餐的一页规格；切换套餐不会保留上一套餐的规格结果。"
    >
      <template #meta>
        <StatusBadge :tone="selectedPlan?.active_sku_count ? 'success' : 'warning'">
          {{ selectedPlan?.active_sku_count || 0 }} 个可售规格
        </StatusBadge>
      </template>

      <PageAlert v-if="selectedError" tone="danger" title="套餐详情加载失败">{{ selectedError }}</PageAlert>

      <DataWorkbench :total="skuTotal" :loading="skuLoading || selectedLoading" :refreshing="skuRefreshing">
        <template #filters>
          <WorkbenchFilterBar :active="Boolean(skuQuery || skuDraftQuery)" @clear="clearSKUFilters">
            <WorkbenchFilterInput v-model="skuDraftQuery" label="搜索" placeholder="规格名称、编码或币种" @apply="applySKUFilters" />
          </WorkbenchFilterBar>
        </template>

        <DataTable v-if="skus.length" caption="当前套餐可购买规格" :row-count="skuTotal" :min-width="900">
          <thead>
            <tr>
              <th class="table-primary-column">规格</th>
              <th>价格 / 周期</th>
              <th>流量</th>
              <th data-column-priority="2">设备</th>
              <th data-column-priority="2">速率</th>
              <th class="table-action-column"><span class="sr-only">操作</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="sku in skus" :key="sku.id">
              <td class="table-primary-column">
                <div class="cell-title"><strong>{{ sku.name }}</strong><span>{{ sku.code }}</span></div>
              </td>
              <td>
                <div class="cell-title">
                  <strong>{{ formatCurrency(sku.price_cents, sku.currency) }}</strong>
                  <span>{{ billingLabel(sku) }}</span>
                </div>
              </td>
              <td>{{ formatBytes(selectedPlan?.traffic_bytes || 0) }}</td>
              <td data-column-priority="2">{{ selectedPlan?.device_limit || '不限' }}</td>
              <td data-column-priority="2">{{ selectedPlan?.speed_limit_mbps ? `${selectedPlan.speed_limit_mbps} Mbps` : '不限速' }}</td>
              <td class="table-action-column">
                <UiButton size="sm" type="button" @click="askOrder(sku)">选择此规格</UiButton>
              </td>
            </tr>
          </tbody>
        </DataTable>
        <EmptyState v-else icon="plans" title="没有可购买规格" description="可以调整关键词，或选择其他套餐。" />

        <template #footer>
          <TablePager
            :total="skuTotal"
            :offset="skuOffset"
            :limit="skuLimit"
            :loading="skuLoading"
            @change="changeSKUPage"
          />
        </template>
      </DataWorkbench>
    </UiSection>

    <EmptyState
      v-else
      icon="plans"
      title="先选择一个套餐"
      description="规格不会随套餐目录一起全部下载；选择套餐后再按页加载。"
    />

    <ConfirmDialog
      :open="confirmOpen"
      title="创建新购订单？"
      :message="confirmMessage"
      confirm-text="确认创建"
      :busy="creating"
      @close="confirmOpen = false"
      @confirm="submitOrder"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  createOrder,
  fetchPlanCatalogItem,
  fetchPlanCatalogPage,
  fetchPlanCatalogSKUs,
  type PlanCatalogItem,
  type PlanSKU,
} from '../../api/client'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
import DataTable from '../../components/DataTable.vue'
import DataWorkbench from '../../components/DataWorkbench.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageAlert from '../../components/PageAlert.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TablePager from '../../components/TablePager.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import UiSection from '../../components/UiSection.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../../components/WorkbenchFilterInput.vue'
import { useRemoteTable } from '../../composables/useRemoteTable'
import { formatBytes, formatCurrency } from '../../utils/format'

const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]

const initialPlanLimit = Number(route.query.limit)
const planLimit = ref(allowedPageSizes.includes(initialPlanLimit) ? initialPlanLimit : 25)
const planOffset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * planLimit.value)
const planQuery = ref(String(route.query.q || '').trim())
const planDraftQuery = ref(planQuery.value)
const selectedPlanID = ref(Math.max(0, Number(route.query.plan) || 0))
const selectedPlan = ref<PlanCatalogItem | null>(null)
const selectedLoading = ref(false)
const selectedError = ref('')
let selectedController: AbortController | null = null

const initialSKULimit = Number(route.query.sku_limit)
const skuLimit = ref(allowedPageSizes.includes(initialSKULimit) ? initialSKULimit : 25)
const skuOffset = ref((Math.max(1, Number(route.query.sku_page) || 1) - 1) * skuLimit.value)
const skuQuery = ref(String(route.query.sku_q || '').trim())
const skuDraftQuery = ref(skuQuery.value)

const creating = ref(false)
const message = ref('')
const actionError = ref('')
const confirmOpen = ref(false)
const selectedSKU = ref<PlanSKU | null>(null)

const {
  items: plans,
  total: planTotal,
  loading: planLoading,
  refreshing: planRefreshing,
  error: planError,
  load: loadPlans,
} = useRemoteTable<PlanCatalogItem>({
  offset: planOffset,
  limit: planLimit,
  fetchPage: ({ signal }) => fetchPlanCatalogPage({
    q: planQuery.value || undefined,
    offset: planOffset.value,
    limit: planLimit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '套餐目录加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

const {
  items: skus,
  total: skuTotal,
  loading: skuLoading,
  refreshing: skuRefreshing,
  error: skuError,
  load: loadSKUs,
  invalidate: invalidateSKUs,
} = useRemoteTable<PlanSKU>({
  offset: skuOffset,
  limit: skuLimit,
  fetchPage: ({ signal }) => fetchPlanCatalogSKUs(selectedPlanID.value, {
    q: skuQuery.value || undefined,
    skuType: 'new',
    offset: skuOffset.value,
    limit: skuLimit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '套餐规格加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

const loading = computed(() => planLoading.value || skuLoading.value || selectedLoading.value)
const error = computed(() => actionError.value || planError.value || skuError.value)
const confirmMessage = computed(() => selectedPlan.value && selectedSKU.value
  ? `将按 ${formatCurrency(selectedSKU.value.price_cents, selectedSKU.value.currency)} 创建“${selectedPlan.value.name} / ${selectedSKU.value.name}”订单。订单创建后等待支付或人工确认。`
  : '')

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

async function syncURL(replace = false) {
  const page = Math.floor(planOffset.value / planLimit.value) + 1
  const skuPage = Math.floor(skuOffset.value / skuLimit.value) + 1
  const location = {
    query: {
      ...(planQuery.value ? { q: planQuery.value } : {}),
      ...(page > 1 ? { page: String(page) } : {}),
      ...(planLimit.value !== 25 ? { limit: String(planLimit.value) } : {}),
      ...(selectedPlanID.value ? { plan: String(selectedPlanID.value) } : {}),
      ...(selectedPlanID.value && skuQuery.value ? { sku_q: skuQuery.value } : {}),
      ...(selectedPlanID.value && skuPage > 1 ? { sku_page: String(skuPage) } : {}),
      ...(selectedPlanID.value && skuLimit.value !== 25 ? { sku_limit: String(skuLimit.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
}

async function loadSelectedPlan() {
  selectedController?.abort()
  selectedPlan.value = null
  selectedError.value = ''
  if (!selectedPlanID.value) {
    invalidateSKUs()
    return
  }
  selectedController = new AbortController()
  const controller = selectedController
  selectedLoading.value = true
  try {
    selectedPlan.value = await fetchPlanCatalogItem(selectedPlanID.value, { signal: controller.signal })
  } catch (cause: any) {
    if (!controller.signal.aborted) selectedError.value = cause?.response?.data?.message || '套餐不存在或已停止销售。'
  } finally {
    if (selectedController === controller) selectedLoading.value = false
  }
}

async function applyPlanFilters() {
  planQuery.value = planDraftQuery.value.trim()
  planOffset.value = 0
  await syncURL()
  await loadPlans()
}

async function clearPlanFilters() {
  planDraftQuery.value = ''
  planQuery.value = ''
  planOffset.value = 0
  await syncURL()
  await loadPlans()
}

async function changePlanPage(value: { offset: number; limit: number }) {
  planOffset.value = value.offset
  planLimit.value = value.limit
  await syncURL()
  await loadPlans()
}

async function selectPlan(plan: PlanCatalogItem) {
  selectedPlanID.value = plan.id
  selectedPlan.value = plan
  selectedError.value = ''
  skuQuery.value = ''
  skuDraftQuery.value = ''
  skuOffset.value = 0
  await syncURL()
  await loadSKUs()
}

async function applySKUFilters() {
  skuQuery.value = skuDraftQuery.value.trim()
  skuOffset.value = 0
  await syncURL()
  await loadSKUs()
}

async function clearSKUFilters() {
  skuDraftQuery.value = ''
  skuQuery.value = ''
  skuOffset.value = 0
  await syncURL()
  await loadSKUs()
}

async function changeSKUPage(value: { offset: number; limit: number }) {
  skuOffset.value = value.offset
  skuLimit.value = value.limit
  await syncURL()
  await loadSKUs()
}

async function refreshAll() {
  actionError.value = ''
  const tasks: Promise<unknown>[] = [loadPlans()]
  if (selectedPlanID.value) tasks.push(loadSelectedPlan(), loadSKUs())
  await Promise.all(tasks)
}

function askOrder(sku: PlanSKU) {
  selectedSKU.value = sku
  confirmOpen.value = true
}

async function submitOrder() {
  if (!selectedSKU.value) return
  creating.value = true
  actionError.value = ''
  try {
    await createOrder(selectedSKU.value.id)
    confirmOpen.value = false
    message.value = '订单已创建，正在前往订单页面。'
    await router.push('/account/orders')
  } catch (cause: any) {
    actionError.value = cause?.response?.data?.message || '订单创建失败。'
  } finally {
    creating.value = false
  }
}

watch(() => route.fullPath, async () => {
  const nextPlanQuery = String(route.query.q || '').trim()
  const rawPlanLimit = Number(route.query.limit)
  const nextPlanLimit = allowedPageSizes.includes(rawPlanLimit) ? rawPlanLimit : 25
  const nextPlanOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextPlanLimit
  const nextSelectedPlanID = Math.max(0, Number(route.query.plan) || 0)
  const nextSKUQuery = String(route.query.sku_q || '').trim()
  const rawSKULimit = Number(route.query.sku_limit)
  const nextSKULimit = allowedPageSizes.includes(rawSKULimit) ? rawSKULimit : 25
  const nextSKUOffset = (Math.max(1, Number(route.query.sku_page) || 1) - 1) * nextSKULimit

  const planListChanged = nextPlanQuery !== planQuery.value || nextPlanLimit !== planLimit.value || nextPlanOffset !== planOffset.value
  const selectedChanged = nextSelectedPlanID !== selectedPlanID.value
  const skuListChanged = nextSKUQuery !== skuQuery.value || nextSKULimit !== skuLimit.value || nextSKUOffset !== skuOffset.value

  planQuery.value = nextPlanQuery
  planDraftQuery.value = nextPlanQuery
  planLimit.value = nextPlanLimit
  planOffset.value = nextPlanOffset
  selectedPlanID.value = nextSelectedPlanID
  skuQuery.value = nextSKUQuery
  skuDraftQuery.value = nextSKUQuery
  skuLimit.value = nextSKULimit
  skuOffset.value = nextSKUOffset

  if (planListChanged) await loadPlans()
  if (selectedChanged) {
    await loadSelectedPlan()
    if (selectedPlanID.value) await loadSKUs()
  } else if (skuListChanged && selectedPlanID.value) {
    await loadSKUs()
  }
})

onMounted(async () => {
  await loadPlans()
  if (selectedPlanID.value) await Promise.all([loadSelectedPlan(), loadSKUs()])
})

onBeforeUnmount(() => selectedController?.abort())
</script>

<style scoped>
.numeric-value {
  font-variant-numeric: tabular-nums;
}
</style>
