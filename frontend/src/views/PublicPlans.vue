<template>
  <div class="pricing-page commerce-pricing-page commerce-catalog-page">
    <PageAlert v-if="detailError && !selectedPlan" tone="danger" title="套餐加载失败">
      {{ detailError }} <UiButton variant="secondary" @click="retryDetail">重试详情</UiButton>
    </PageAlert>
    <div v-if="detailLoading && !selectedPlan" class="commerce-loading-state" role="status">
      <UiIcon name="refresh" />正在加载套餐详情
    </div>
    <template v-else-if="selectedPlan">
      <CommercePlanDetail
        :plan="selectedPlan"
        :skus="detailSKUs"
        :selected-sku-id="selectedSKUID"
        :loading="detailLoading"
        :error="detailError"
        :sku-total="skuTotal"
        :sku-offset="skuOffset"
        :sku-limit="skuLimit"
        @change-sku-page="changeSKUPage"
        @retry="retryDetail"
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

      <PageAlert v-if="error" class="pricing-alert" tone="danger" title="套餐加载失败">
        {{ error }} <UiButton variant="secondary" @click="retryCatalog">重试目录</UiButton>
      </PageAlert>

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
          :disabled="Boolean(error)"
          @select="openDetail(plan.id)"
        />
      </section>

      <div v-else-if="!error" class="public-empty commerce-catalog-empty">
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
import { onScopeDispose, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchPlanCatalogPage, type PlanCatalogItem, type PlanSKU } from '../api/client'
import CommercePlanCard from '../components/CommercePlanCard.vue'
import CommercePlanDetail from '../components/CommercePlanDetail.vue'
import PageAlert from '../components/PageAlert.vue'
import TablePager from '../components/TablePager.vue'
import UiButton from '../components/UiButton.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import { useCatalogDetail } from '../composables/useCatalogDetail'
import { useRemoteTable } from '../composables/useRemoteTable'
import { useAppStore } from '../stores/app'

const app = useAppStore(), route = useRoute(), router = useRouter()
const limit = ref(9), offset = ref(0), query = ref(''), searchDraft = ref('')
const catalog = useRemoteTable<PlanCatalogItem>({
  offset, limit,
  fetchPage: ({ signal }) => fetchPlanCatalogPage({ q: query.value || undefined, offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: '套餐加载失败，请重试。',
})
const { items: plans, total, loading, error } = catalog
const detail = useCatalogDetail()
const { plan: selectedPlan, skus: detailSKUs, selectedSkuId: selectedSKUID, selectedSku: selectedSKU,
  loading: detailLoading, error: detailError, total: skuTotal, offset: skuOffset, limit: skuLimit } = detail
function catalogQuery() {
  return { ...(query.value ? { q: query.value } : {}), page: String(Math.floor(offset.value / limit.value) + 1), limit: String(limit.value) }
}
function syncDetailURL() {
  if (selectedPlan.value) return router.replace({ query: { ...catalogQuery(), plan: String(selectedPlan.value.id), sku: String(selectedSKUID.value) } })
}
function openDetail(id: number) { return router.replace({ query: { ...catalogQuery(), plan: String(id) } }) }
function closeDetail() { return router.replace({ query: catalogQuery() }) }
function selectSKU(sku: PlanSKU) {
  if (detailLoading.value || detailError.value || !detailSKUs.value.some(item => item.id === sku.id)) return
  selectedSKUID.value = sku.id
  return syncDetailURL()
}
async function changeSKUPage(value: { offset: number; limit: number }) {
  if (await detail.changePage(value)) await syncDetailURL()
}
async function retryDetail() { if (await detail.retry()) await syncDetailURL() }
async function continuePurchase() {
  if (!selectedPlan.value || !selectedSKU.value) return
  const target = `/account/plans?operation=purchase&plan=${selectedPlan.value.id}&sku=${selectedSKU.value.id}&step=detail`
  await router.push(app.isAuthenticated ? target : `/login?redirect=${encodeURIComponent(target)}`)
}
function submitSearch() {
  return router.replace({ query: { ...catalogQuery(), q: searchDraft.value.trim() || undefined, page: '1' } })
}
function clearSearch() { searchDraft.value = ''; return submitSearch() }
function changePage(value: { offset: number; limit: number }) {
  return router.replace({ query: { ...catalogQuery(), page: String(Math.floor(value.offset / value.limit) + 1), limit: String(value.limit) } })
}
let generation = 0, catalogKey = ''
onScopeDispose(() => { generation++ })
async function applyRoute(force = false) {
  const current = ++generation
  limit.value = [6, 9, 12].includes(Number(route.query.limit)) ? Number(route.query.limit) : 9
  offset.value = (Math.max(1, Number(route.query.page) || 1) - 1) * limit.value
  query.value = String(route.query.q || '').trim()
  searchDraft.value = query.value
  const planID = Math.max(0, Number(route.query.plan) || 0), skuID = Math.max(0, Number(route.query.sku) || 0)
  if (planID) {
    catalog.invalidate()
    catalogKey = ''
    if (!force && selectedPlan.value?.id === planID && (!skuID || detailSKUs.value.some(item => item.id === skuID))) {
      if (skuID) selectedSKUID.value = skuID
    } else {
      if (!await detail.open(planID, 'purchase', skuID) || current !== generation) return
    }
    return
  }
  detail.close()
  const nextCatalog = `${query.value}:${offset.value}:${limit.value}`
  if (force || catalogKey !== nextCatalog) {
    catalogKey = nextCatalog
    await catalog.load()
  }
}
function retryCatalog() { return applyRoute(true) }
watch(() => route.fullPath, () => { void applyRoute() }, { immediate: true })
</script>
