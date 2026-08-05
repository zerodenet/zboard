<template>
  <div class="pricing-page commerce-pricing-page commerce-catalog-page">
    <section class="pricing-hero">
      <span>套餐与价格</span>
      <h1>选择适合你的套餐</h1>
      <p>先比较套餐权益，再在同一张卡片中选择月付、季付或年付周期。</p>
    </section>

    <PageAlert v-if="error" class="pricing-alert" tone="danger" title="套餐加载失败">{{ error }}</PageAlert>

    <div v-if="loading" class="commerce-loading-state" role="status">
      <UiIcon name="refresh" />正在加载可购买套餐
    </div>

    <section v-else-if="plans.length" class="commerce-catalog-grid" aria-label="可购买套餐">
      <article v-for="plan in plans" :key="plan.id" class="commerce-product-card">
        <header class="commerce-product-head">
          <div>
            <span>{{ plan.slug }}</span>
            <h2>{{ plan.name }}</h2>
          </div>
          <small v-if="offersFor(plan.id).length">{{ offersFor(plan.id).length }} 个计费周期</small>
        </header>

        <p class="commerce-product-summary">{{ plan.summary || '稳定、透明的订阅服务。' }}</p>

        <div v-if="offerState[plan.id]?.loading" class="commerce-card-loading">
          <UiIcon name="refresh" />正在加载价格
        </div>
        <template v-else-if="selectedOffer(plan.id)">
          <div class="commerce-product-price">
            <strong>{{ formatCurrency(selectedOffer(plan.id)!.price_cents, selectedOffer(plan.id)!.currency) }}</strong>
            <span>/ {{ billingLabel(selectedOffer(plan.id)!) }}</span>
          </div>

          <div class="commerce-offer-tabs" role="radiogroup" :aria-label="`${plan.name} 计费周期`">
            <button
              v-for="sku in offersFor(plan.id)"
              :key="sku.id"
              type="button"
              role="radio"
              :aria-checked="selectedOfferIDs[plan.id] === sku.id"
              :class="{ active: selectedOfferIDs[plan.id] === sku.id }"
              @click="selectedOfferIDs[plan.id] = sku.id"
            >
              <span>{{ sku.name }}</span>
              <strong>{{ formatCurrency(sku.price_cents, sku.currency) }}</strong>
            </button>
          </div>
        </template>
        <PageAlert v-else-if="offerState[plan.id]?.error" tone="danger" title="价格加载失败">
          {{ offerState[plan.id]?.error }}
        </PageAlert>
        <div v-else class="commerce-unavailable-price">
          <strong>暂不可购买</strong>
          <span>当前没有可用的新购规格</span>
        </div>

        <ul class="commerce-entitlement-list">
          <li><UiIcon name="check" />{{ formatBytes(plan.traffic_bytes) }} 套餐流量</li>
          <li><UiIcon name="check" />{{ plan.device_limit > 0 ? `${plan.device_limit} 台设备` : '不限设备' }}</li>
          <li><UiIcon name="check" />{{ plan.speed_limit_mbps ? `${plan.speed_limit_mbps} Mbps` : '不限速' }}</li>
        </ul>

        <div class="commerce-card-footer">
          <UiButton
            type="button"
            :disabled="!selectedOffer(plan.id)"
            @click="chooseOffer(plan.id)"
          >
            {{ app.isAuthenticated ? '选择此套餐' : '登录后选择' }}
            <UiIcon name="chevron" />
          </UiButton>
        </div>
      </article>
    </section>

    <div v-else-if="!loading" class="public-empty commerce-catalog-empty">
      <span><UiIcon name="plans" /></span>
      <h2>套餐正在准备中</h2>
      <p>管理员发布可购买套餐后会显示在这里。</p>
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

    <section class="pricing-note">
      <UiIcon name="shield" />
      <div>
        <strong>价格与套餐权益分别确认</strong>
        <p>选择计费周期后进入结算页，最终确认价格、服务期限和套餐权益后才创建订单。</p>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchPlanCatalogPage, fetchPlanCatalogSKUs, type PlanCatalogItem, type PlanSKU } from '../api/client'
import PageAlert from '../components/PageAlert.vue'
import TablePager from '../components/TablePager.vue'
import UiButton from '../components/UiButton.vue'
import UiIcon from '../components/UiIcon.vue'
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
const allowedPageSizes = [6, 9, 12]
const routeLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(routeLimit) ? routeLimit : 9)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const plans = ref<PlanCatalogItem[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const offerState = reactive<Record<number, OfferState>>({})
const selectedOfferIDs = reactive<Record<number, number>>({})

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

function offersFor(planID: number) {
  return offerState[planID]?.items || []
}

function selectedOffer(planID: number) {
  const offers = offersFor(planID)
  return offers.find(item => item.id === selectedOfferIDs[planID]) || offers[0] || null
}

async function loadOffers(planID: number) {
  offerState[planID] = { loading: true, error: '', items: [] }
  try {
    const result = await fetchPlanCatalogSKUs(planID, { skuType: 'new', offset: 0, limit: 100 })
    offerState[planID] = { loading: false, error: '', items: result.items }
    selectedOfferIDs[planID] = result.items[0]?.id || 0
  } catch (cause: any) {
    offerState[planID] = {
      loading: false,
      error: cause?.response?.data?.message || '计费周期加载失败。',
      items: [],
    }
    selectedOfferIDs[planID] = 0
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await fetchPlanCatalogPage({ offset: offset.value, limit: limit.value })
    plans.value = result.items
    total.value = result.total
    await Promise.all(plans.value.map(plan => loadOffers(plan.id)))
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '套餐加载失败。'
    plans.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function chooseOffer(planID: number) {
  const offer = selectedOffer(planID)
  if (!offer) return
  const target = `/account/plans?operation=purchase&plan=${planID}&sku=${offer.id}`
  await router.push(app.isAuthenticated ? target : `/login?redirect=${encodeURIComponent(target)}`)
}

async function syncURL() {
  const page = Math.floor(offset.value / limit.value) + 1
  await router.replace({
    query: {
      ...(page > 1 ? { page: String(page) } : {}),
      ...(limit.value !== 9 ? { limit: String(limit.value) } : {}),
    },
  })
}

async function changePage(value: { offset: number; limit: number }) {
  offset.value = value.offset
  limit.value = value.limit
  await syncURL()
  await load()
}

onMounted(load)
</script>
