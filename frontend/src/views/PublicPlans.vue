<template>
  <div class="pricing-page">
    <section class="pricing-hero">
      <span>套餐与价格</span>
      <h1>按你的使用方式选择</h1>
      <p>价格、周期、流量和设备限制在下单前完整展示；目录采用分页加载，不会因套餐数量增长而拖慢页面。</p>
    </section>

    <PageAlert v-if="error" class="pricing-alert" tone="danger" title="套餐加载失败">{{ error }}</PageAlert>

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(query || draftQuery)" @clear="clearSearch">
          <WorkbenchFilterInput v-model="draftQuery" label="搜索" placeholder="套餐名称或简介" @apply="applySearch" />
        </WorkbenchFilterBar>
      </template>

      <section v-if="plans.length" class="public-plan-grid">
        <article v-for="(plan, index) in plans" :key="plan.id" :class="{ featured: offset === 0 && index === 0 }">
          <span v-if="offset === 0 && index === 0" class="plan-ribbon">推荐</span>
          <div class="public-plan-head">
            <span>{{ plan.slug }}</span>
            <h2>{{ plan.name }}</h2>
            <p>{{ plan.summary || '稳定、透明的订阅服务。' }}</p>
          </div>
          <div v-if="plan.primary_sku" class="public-price">
            <strong>{{ formatCurrency(plan.primary_sku.price_cents, plan.primary_sku.currency) }}</strong>
            <span>起 / {{ billingLabel(plan.primary_sku) }}</span>
          </div>
          <ul v-if="plan.primary_sku">
            <li><UiIcon name="check" />{{ formatBytes(plan.traffic_bytes) }} 套餐流量</li>
            <li><UiIcon name="check" />{{ plan.device_limit || '不限' }} 台设备</li>
            <li><UiIcon name="check" />{{ plan.speed_limit_mbps ? `${plan.speed_limit_mbps} Mbps` : '不限速' }}</li>
            <li><UiIcon name="check" />{{ plan.active_sku_count }} 个可选规格</li>
          </ul>
          <p v-else class="field-hint">当前套餐暂时没有可购买的新购规格。</p>
          <RouterLink
            class="button"
            :class="{ 'button-secondary': !(offset === 0 && index === 0) }"
            :to="purchasePath(plan.id)"
          >
            {{ app.isAuthenticated ? '查看规格并购买' : '登录后查看规格' }}
          </RouterLink>
        </article>
      </section>
      <div v-else-if="!loading" class="public-empty">
        <span><UiIcon name="plans" /></span>
        <h2>{{ query ? '没有匹配的套餐' : '套餐正在准备中' }}</h2>
        <p>{{ query ? '换一个关键词，或清空搜索后重试。' : '管理员发布可售套餐后会显示在这里。' }}</p>
      </div>

      <template #footer>
        <TablePager
          :total="total"
          :offset="offset"
          :limit="limit"
          :loading="loading"
          :page-sizes="[6, 12, 24]"
          @change="changePage"
        />
      </template>
    </DataWorkbench>

    <section class="pricing-note">
      <UiIcon name="shield" />
      <div>
        <strong>购买前信息完全透明</strong>
        <p>订单会保留套餐与规格快照，后续调整不会改变已经创建的订单。</p>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchPlanCatalogPage, type PlanCatalogItem, type PlanSKU } from '../api/client'
import DataWorkbench from '../components/DataWorkbench.vue'
import PageAlert from '../components/PageAlert.vue'
import TablePager from '../components/TablePager.vue'
import UiButton from '../components/UiButton.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import { useRemoteTable } from '../composables/useRemoteTable'
import { useAppStore } from '../stores/app'
import { formatBytes, formatCurrency } from '../utils/format'

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const allowedPageSizes = [6, 12, 24]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 6)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const query = ref(String(route.query.q || '').trim())
const draftQuery = ref(query.value)

const {
  items: plans,
  total,
  loading,
  refreshing,
  error,
  load,
} = useRemoteTable<PlanCatalogItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchPlanCatalogPage({
    q: query.value || undefined,
    offset: offset.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '套餐加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

function purchasePath(planID: number) {
  const target = `/account/plans?plan=${planID}`
  return app.isAuthenticated ? target : `/login?redirect=${encodeURIComponent(target)}`
}

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = {
    query: {
      ...(query.value ? { q: query.value } : {}),
      ...(page > 1 ? { page: String(page) } : {}),
      ...(limit.value !== 6 ? { limit: String(limit.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
}

async function applySearch() {
  query.value = draftQuery.value.trim()
  offset.value = 0
  await syncURL()
  await load()
}

async function clearSearch() {
  query.value = ''
  draftQuery.value = ''
  offset.value = 0
  await syncURL()
  await load()
}

async function changePage(value: { offset: number; limit: number }) {
  offset.value = value.offset
  limit.value = value.limit
  await syncURL()
  await load()
}

watch(() => route.fullPath, async () => {
  const nextQuery = String(route.query.q || '').trim()
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 6
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  if (nextQuery !== query.value || nextLimit !== limit.value || nextOffset !== offset.value) {
    query.value = nextQuery
    draftQuery.value = nextQuery
    limit.value = nextLimit
    offset.value = nextOffset
    await load()
  }
})

onMounted(load)
</script>
