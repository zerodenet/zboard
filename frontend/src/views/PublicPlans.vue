<template>
  <div class="pricing-page commerce-pricing-page">
    <section class="pricing-hero">
      <span>套餐与价格</span>
      <h1>先选套餐，再选计费周期</h1>
      <p>套餐负责流量、设备数和限速；月付、季付和年付只改变价格与服务周期。</p>
    </section>

    <PageAlert v-if="error" class="pricing-alert" tone="danger" title="套餐加载失败">{{ error }}</PageAlert>

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(query || draftQuery)" @clear="clearSearch">
          <WorkbenchFilterInput v-model="draftQuery" label="搜索" placeholder="套餐名称或简介" @apply="applySearch" />
        </WorkbenchFilterBar>
      </template>

      <section v-if="plans.length" class="public-plan-grid commerce-public-plan-grid">
        <article
          v-for="(plan, index) in plans"
          :key="plan.id"
          class="commerce-public-plan-card"
          :class="{ featured: offset === 0 && index === 0, expanded: expandedPlanID === plan.id }"
        >
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
          <div v-else class="public-price public-price-muted">
            <strong>暂未定价</strong>
            <span>等待销售规格上架</span>
          </div>

          <ul>
            <li><UiIcon name="check" />{{ formatBytes(plan.traffic_bytes) }} 套餐流量</li>
            <li><UiIcon name="check" />{{ plan.device_limit || '不限' }} 台设备</li>
            <li><UiIcon name="check" />{{ plan.speed_limit_mbps ? `${plan.speed_limit_mbps} Mbps` : '不限速' }}</li>
            <li><UiIcon name="check" />{{ plan.active_sku_count }} 个可购买周期</li>
          </ul>

          <div class="commerce-card-actions">
            <UiButton
              v-if="plan.primary_sku"
              variant="secondary"
              type="button"
              :loading="offerState[plan.id]?.loading"
              @click="toggleOffers(plan.id)"
            >
              {{ expandedPlanID === plan.id ? '收起计费周期' : '查看计费周期' }}
            </UiButton>
            <UiButton
              v-if="plan.primary_sku"
              type="button"
              @click="chooseOffer(plan.id, plan.primary_sku.id)"
            >
              {{ app.isAuthenticated ? '按起售价购买' : '登录后购买' }}
            </UiButton>
          </div>

          <div v-if="expandedPlanID === plan.id" class="public-plan-offers" aria-live="polite">
            <PageAlert v-if="offerState[plan.id]?.error" tone="danger" title="计费周期加载失败">
              {{ offerState[plan.id]?.error }}
            </PageAlert>
            <div v-else-if="offerState[plan.id]?.items.length" class="public-offer-grid">
              <button
                v-for="sku in offerState[plan.id].items"
                :key="sku.id"
                type="button"
                class="public-offer-option"
                @click="chooseOffer(plan.id, sku.id)"
              >
                <span>
                  <strong>{{ sku.name }}</strong>
                  <small>{{ billingLabel(sku) }}</small>
                </span>
                <b>{{ formatCurrency(sku.price_cents, sku.currency) }}</b>
                <UiIcon name="chevron" />
              </button>
            </div>
            <p v-else-if="!offerState[plan.id]?.loading" class="field-hint">当前套餐没有可购买的周期规格。</p>
          </div>
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
        <strong>商品权益和计费周期分开确认</strong>
        <p>进入账户后会再次展示套餐权益、计费周期、价格和操作类型，确认后才创建订单。</p>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchPlanCatalogPage, fetchPlanCatalogSKUs, type PlanCatalogItem, type PlanSKU } from '../api/client'
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

interface OfferState {
  loading: boolean
  error: string
  items: PlanSKU[]
}

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const allowedPageSizes = [6, 12, 24]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 6)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const query = ref(String(route.query.q || '').trim())
const draftQuery = ref(query.value)
const expandedPlanID = ref(0)
const offerState = reactive<Record<number, OfferState>>({})

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
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

function purchasePath(planID: number, skuID?: number) {
  const params = new URLSearchParams({ plan: String(planID), operation: 'purchase' })
  if (skuID) params.set('sku', String(skuID))
  return `/account/plans?${params}`
}

async function chooseOffer(planID: number, skuID?: number) {
  const target = purchasePath(planID, skuID)
  await router.push(app.isAuthenticated ? target : `/login?redirect=${encodeURIComponent(target)}`)
}

async function toggleOffers(planID: number) {
  if (expandedPlanID.value === planID) {
    expandedPlanID.value = 0
    return
  }
  expandedPlanID.value = planID
  if (offerState[planID]?.items.length || offerState[planID]?.loading) return
  offerState[planID] = { loading: true, error: '', items: [] }
  try {
    const result = await fetchPlanCatalogSKUs(planID, { skuType: 'new', offset: 0, limit: 100 })
    offerState[planID] = { loading: false, error: '', items: result.items }
  } catch (cause: any) {
    offerState[planID] = {
      loading: false,
      error: cause?.response?.data?.message || '计费周期加载失败。',
      items: [],
    }
  }
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
  expandedPlanID.value = 0
  await syncURL()
  await load()
}

async function clearSearch() {
  query.value = ''
  draftQuery.value = ''
  offset.value = 0
  expandedPlanID.value = 0
  await syncURL()
  await load()
}

async function changePage(value: { offset: number; limit: number }) {
  offset.value = value.offset
  limit.value = value.limit
  expandedPlanID.value = 0
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
    expandedPlanID.value = 0
    await load()
  }
})

onMounted(load)
</script>
