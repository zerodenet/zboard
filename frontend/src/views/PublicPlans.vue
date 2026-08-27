<template>
  <div class="pricing-page commerce-pricing-page commerce-catalog-page">
    <PageAlert v-if="detailError" tone="danger" title="套餐加载失败">{{ detailError }}</PageAlert>
    <div v-if="detailLoading && !selectedPlan" class="commerce-loading-state" role="status">
      <UiIcon name="refresh" />正在加载套餐详情
    </div>
    <template v-else-if="selectedPlan">
      <CommercePlanDetail
        :plan="selectedPlan"
        :skus="detailSKUs"
        :selected-sku-id="selectedSKUID"
        :loading="detailLoading"
        :error="skuError"
        operation-label="新购"
        @back="closeDetail"
        @select-sku="selectSKU"
        @continue="continuePurchase"
      />
    </template>

    <template v-else>
      <section class="pricing-hero">
        <span>套餐与价格</span>
        <h1>选择适合你的服务方案</h1>
        <p>比较流量、速度、设备数与价格，按需求选择。</p>
      </section>

      <WorkbenchFilterBar
        :active="Boolean(query)"
        :loading="loading"
        label="套餐筛选"
        @clear="clearSearch"
      >
        <WorkbenchFilterInput
          v-model="searchDraft"
          label="搜索"
          placeholder="搜索套餐名称或介绍"
          @apply="submitSearch"
        />
      </WorkbenchFilterBar>

      <PageAlert v-if="error" class="pricing-alert" tone="danger" title="套餐加载失败">{{ error }}</PageAlert>

      <div v-if="loading" class="commerce-loading-state" role="status">
        <UiIcon name="refresh" />正在加载套餐
      </div>

      <section v-else-if="plans.length" class="commerce-catalog-grid" aria-label="套餐列表">
        <CommercePlanCard
          v-for="plan in plans"
          :key="plan.id"
          :plan="plan"
          :offer="plan.primary_sku || null"
          :offer-count="plan.active_sku_count"
          @select="openDetail(plan.id)"
        />
      </section>

      <div v-else class="public-empty commerce-catalog-empty">
        <span><UiIcon name="plans" /></span>
        <h2>{{ query ? '没有找到匹配的套餐' : '暂时没有可用套餐' }}</h2>
        <p>{{ query ? '请尝试其他关键词。' : '新的套餐上线后会在这里展示。' }}</p>
      </div>

      <TablePager
        v-if="total > limit"
        :total="total"
        :offset="offset"
        :limit="limit"
        :loading="loading"
        :page-sizes="[6, 9, 12]"
        @change="changePage"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  fetchPlanCatalogItem,
  fetchPlanCatalogPage,
  fetchPlanCatalogSKUs,
  type PlanCatalogItem,
  type PlanSKU,
} from '../api/client'
import CommercePlanCard from '../components/CommercePlanCard.vue'
import CommercePlanDetail from '../components/CommercePlanDetail.vue'
import PageAlert from '../components/PageAlert.vue'
import TablePager from '../components/TablePager.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const allowedPageSizes = [6, 9, 12]
const routeLimit = Number(route.query.limit)

const limit = ref(allowedPageSizes.includes(routeLimit) ? routeLimit : 9)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const query = ref(String(route.query.q || '').trim())
const searchDraft = ref(query.value)
const plans = ref<PlanCatalogItem[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')

const selectedPlan = ref<PlanCatalogItem | null>(null)
const detailSKUs = ref<PlanSKU[]>([])
const selectedSKUID = ref(Math.max(0, Number(route.query.sku) || 0))
const detailLoading = ref(false)
const detailError = ref('')
const skuError = ref('')

async function syncCatalogURL() {
  const page = Math.floor(offset.value / limit.value) + 1
  await router.replace({
    query: {
      ...(query.value ? { q: query.value } : {}),
      ...(page > 1 ? { page: String(page) } : {}),
      ...(limit.value !== 9 ? { limit: String(limit.value) } : {}),
    },
  })
}

async function syncDetailURL() {
  if (!selectedPlan.value) return
  await router.replace({
    query: {
      ...(query.value ? { q: query.value } : {}),
      plan: String(selectedPlan.value.id),
      ...(selectedSKUID.value ? { sku: String(selectedSKUID.value) } : {}),
    },
  })
}

async function loadCatalog() {
  loading.value = true
  error.value = ''
  try {
    const result = await fetchPlanCatalogPage({ q: query.value || undefined, offset: offset.value, limit: limit.value })
    plans.value = result.items
    total.value = result.total
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '套餐加载失败。'
    plans.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function openDetail(planID: number, preserveRequestedSKU = false, updateRoute = true) {
  detailLoading.value = true
  detailError.value = ''
  skuError.value = ''
  selectedPlan.value = null
  detailSKUs.value = []
  try {
    const [plan, skuResult] = await Promise.all([
      fetchPlanCatalogItem(planID),
      fetchPlanCatalogSKUs(planID, { operation: 'purchase', offset: 0, limit: 100 }),
    ])
    selectedPlan.value = plan
    detailSKUs.value = skuResult.items
    const requested = preserveRequestedSKU
      ? skuResult.items.find(item => item.id === selectedSKUID.value)
      : undefined
    selectedSKUID.value = requested?.id || skuResult.items[0]?.id || 0
    if (updateRoute) await syncDetailURL()
  } catch (cause: any) {
    detailError.value = cause?.response?.data?.message || '套餐详情加载失败。'
  } finally {
    detailLoading.value = false
  }
}

async function closeDetail() {
  selectedPlan.value = null
  detailSKUs.value = []
  selectedSKUID.value = 0
  await syncCatalogURL()
  if (!plans.value.length) await loadCatalog()
}

async function selectSKU(sku: PlanSKU) {
  selectedSKUID.value = sku.id
  await syncDetailURL()
}

async function continuePurchase() {
  if (!selectedPlan.value || !selectedSKUID.value) return
  const target = `/account/plans?operation=purchase&plan=${selectedPlan.value.id}&sku=${selectedSKUID.value}&step=detail`
  await router.push(app.isAuthenticated ? target : `/login?redirect=${encodeURIComponent(target)}`)
}

async function submitSearch() {
  query.value = searchDraft.value.trim()
  searchDraft.value = query.value
  offset.value = 0
  await syncCatalogURL()
  await loadCatalog()
}

async function clearSearch() {
  searchDraft.value = ''
  query.value = ''
  offset.value = 0
  await syncCatalogURL()
  await loadCatalog()
}

async function changePage(value: { offset: number; limit: number }) {
  offset.value = value.offset
  limit.value = value.limit
  await syncCatalogURL()
  await loadCatalog()
}

watch(
  () => [route.query.q, route.query.page, route.query.limit, route.query.plan, route.query.sku],
  async () => {
    const nextLimitValue = Number(route.query.limit)
    const nextLimit = allowedPageSizes.includes(nextLimitValue) ? nextLimitValue : 9
    const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
    const nextQuery = String(route.query.q || '').trim()
    const nextPlanID = Math.max(0, Number(route.query.plan) || 0)
    const nextSKUID = Math.max(0, Number(route.query.sku) || 0)

    const catalogChanged = query.value !== nextQuery || limit.value !== nextLimit || offset.value !== nextOffset
    query.value = nextQuery
    searchDraft.value = nextQuery
    limit.value = nextLimit
    offset.value = nextOffset

    if (nextPlanID) {
      if (selectedPlan.value?.id !== nextPlanID) {
        selectedSKUID.value = nextSKUID
        await openDetail(nextPlanID, true, false)
      } else if (nextSKUID && detailSKUs.value.some(item => item.id === nextSKUID)) {
        selectedSKUID.value = nextSKUID
      }
      return
    }

    const wasShowingDetail = Boolean(selectedPlan.value)
    if (wasShowingDetail) {
      selectedPlan.value = null
      detailSKUs.value = []
      selectedSKUID.value = 0
    }
    if (catalogChanged || wasShowingDetail) await loadCatalog()
  },
)

onMounted(async () => {
  const requestedPlanID = Math.max(0, Number(route.query.plan) || 0)
  if (requestedPlanID) {
    await openDetail(requestedPlanID, true)
    return
  }
  await loadCatalog()
  await syncCatalogURL()
})
</script>
