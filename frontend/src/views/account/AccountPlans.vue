<template>
  <section class="account-page commerce-account-page commerce-hub-page stack">
    <PageHeader
      :title="pageTitle"
      :description="pageDescription"
      eyebrow="PLANS"
    >
      <template #actions>
        <UiButton v-if="checkoutOpen" variant="secondary" type="button" :disabled="creating" @click="backToDetail">
          返回商品详情
        </UiButton>
        <UiButton v-else-if="selectedPlan" variant="secondary" type="button" @click="backToCatalog">
          返回套餐列表
        </UiButton>
        <UiButton v-else-if="operation !== 'purchase'" variant="secondary" type="button" @click="returnToOverview">
          返回套餐中心
        </UiButton>
        <UiButton v-if="!selectedPlan && !checkoutOpen" variant="secondary" type="button" :disabled="loading" @click="refreshAll">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <PageAlert v-if="actionError && !checkoutOpen" tone="danger" title="套餐操作失败">{{ actionError }}</PageAlert>
    <PageAlert v-if="subscriptionError" tone="danger" title="订阅加载失败">{{ subscriptionError }}</PageAlert>
    <PageAlert v-if="planError" tone="danger" title="套餐目录加载失败">{{ planError }}</PageAlert>

    <section v-if="checkoutOpen && selectedPlan && selectedSKU" class="purchase-checkout">
      <ol class="purchase-checkout__steps" aria-label="结算进度">
        <li class="complete"><span>1</span>选择套餐</li>
        <li class="complete"><span>2</span>选择规格</li>
        <li class="active"><span>3</span>确认订单</li>
      </ol>

      <div class="purchase-checkout__layout">
        <div class="purchase-checkout__main">
          <section class="purchase-checkout__section">
            <header>
              <span>{{ currentOperation.label }}</span>
              <h2>订单确认</h2>
            </header>

            <article class="purchase-checkout__product">
              <div>
                <small>{{ selectedPlan.slug }}</small>
                <h3>{{ selectedPlan.name }}</h3>
                <p>{{ selectedSKU.name }} · {{ billingLabel(selectedSKU) }}</p>
              </div>
              <strong>{{ formatCurrency(selectedSKU.price_cents, selectedSKU.currency) }}</strong>
            </article>

            <dl class="purchase-checkout__details">
              <div v-if="selectedSubscription"><dt>目标订阅</dt><dd>{{ selectedSubscription.plan_name }} / #{{ selectedSubscription.id }}</dd></div>
              <div><dt>订单类型</dt><dd>{{ currentOperation.label }}</dd></div>
              <div><dt>规格</dt><dd>{{ selectedSKU.name }}</dd></div>
              <div><dt>服务周期</dt><dd>{{ billingLabel(selectedSKU) }}</dd></div>
              <template v-if="operation === 'addon'">
                <div><dt>附加流量</dt><dd>{{ formatBytes(selectedSKU.grant_traffic_bytes) }}</dd></div>
                <div><dt>到期时间</dt><dd>保持目标订阅不变</dd></div>
              </template>
              <template v-else>
                <div><dt>套餐流量</dt><dd>{{ formatBytes(selectedPlan.traffic_bytes) }}</dd></div>
                <div><dt>设备数</dt><dd>{{ selectedPlan.device_limit > 0 ? `${selectedPlan.device_limit} 台` : '不限设备' }}</dd></div>
                <div><dt>速度</dt><dd>{{ selectedPlan.speed_limit_mbps > 0 ? `${selectedPlan.speed_limit_mbps} Mbps` : '不限速' }}</dd></div>
              </template>
            </dl>
          </section>
        </div>

        <aside class="purchase-checkout__summary">
          <span>应付金额</span>
          <strong>{{ formatCurrency(selectedSKU.price_cents, selectedSKU.currency) }}</strong>
          <dl>
            <div><dt>商品</dt><dd>{{ selectedPlan.name }}</dd></div>
            <div><dt>规格</dt><dd>{{ selectedSKU.name }}</dd></div>
          </dl>
          <PageAlert v-if="checkoutError" tone="danger" title="无法创建订单">{{ checkoutError }}</PageAlert>
          <UiButton type="button" :loading="creating" @click="submitOrder">确认创建订单</UiButton>
          <p>订单创建后可在“我的订单”中查看状态。</p>
        </aside>
      </div>
    </section>

    <CommercePlanDetail
      v-else-if="selectedPlan"
      :plan="selectedPlan"
      :skus="detailSKUs"
      :selected-sku-id="selectedSKUID"
      :operation-label="currentOperation.label"
      :mode="operation"
      :target-name="selectedSubscription ? `${selectedSubscription.plan_name} / #${selectedSubscription.id}` : ''"
      :loading="detailLoading"
      :error="detailError"
      @back="backToCatalog"
      @select-sku="selectDetailSKU"
      @continue="openCheckout"
    />

    <template v-else>
      <section v-if="operation === 'purchase'" class="commerce-hub-section" aria-labelledby="active-subscriptions-title">
        <div class="commerce-hub-heading">
          <div>
            <span>当前服务</span>
            <h2 id="active-subscriptions-title">管理现有订阅</h2>
            <p>续费、切换套餐和购买流量包从具体订阅发起。</p>
          </div>
          <small v-if="subscriptions.length">{{ subscriptions.length }} 个有效订阅</small>
        </div>

        <div v-if="subscriptionLoading" class="commerce-loading-state"><UiIcon name="refresh" />正在加载订阅</div>
        <div v-else-if="subscriptions.length" class="commerce-subscription-cards">
          <article v-for="subscription in subscriptions" :key="subscription.id" class="commerce-subscription-card">
            <header>
              <div>
                <span>订阅 #{{ subscription.id }}</span>
                <h3>{{ subscription.plan_name }}</h3>
                <p>{{ subscription.sku_name }}</p>
              </div>
              <strong>{{ formatDate(subscription.end_at) }}</strong>
            </header>
            <dl>
              <div><dt>剩余流量</dt><dd>{{ formatBytes(Math.max(0, subscription.flow_total - subscription.flow_used)) }}</dd></div>
              <div><dt>设备数</dt><dd>{{ subscription.device_limit > 0 ? subscription.device_limit : '不限' }}</dd></div>
            </dl>
            <div class="commerce-subscription-actions">
              <UiButton variant="secondary" type="button" @click="startOperation('renew', subscription.id)">续费</UiButton>
              <UiButton variant="secondary" type="button" @click="startOperation('change', subscription.id)">切换套餐</UiButton>
              <UiButton variant="secondary" type="button" @click="startOperation('addon', subscription.id)">购买流量包</UiButton>
            </div>
          </article>
        </div>
        <EmptyState
          v-else-if="subscriptionsLoaded"
          icon="plans"
          title="当前没有有效订阅"
          description="从下方选择套餐即可创建新的订阅。"
        />
      </section>

      <section v-else class="commerce-hub-section" aria-labelledby="target-subscription-title">
        <div class="commerce-hub-heading">
          <div>
            <span>操作对象</span>
            <h2 id="target-subscription-title">{{ selectedSubscription ? '目标订阅' : '选择目标订阅' }}</h2>
            <p>{{ selectedSubscription ? '本次操作只作用于这条订阅。' : '请选择需要处理的订阅。' }}</p>
          </div>
        </div>

        <div v-if="subscriptionLoading" class="commerce-loading-state"><UiIcon name="refresh" />正在加载订阅</div>
        <div v-else-if="selectedSubscription" class="commerce-context-card">
          <div>
            <span>订阅 #{{ selectedSubscription.id }}</span>
            <h3>{{ selectedSubscription.plan_name }}</h3>
            <p>{{ selectedSubscription.sku_name }} · 到期 {{ formatDate(selectedSubscription.end_at) }}</p>
          </div>
          <dl>
            <div><dt>剩余流量</dt><dd>{{ formatBytes(Math.max(0, selectedSubscription.flow_total - selectedSubscription.flow_used)) }}</dd></div>
            <div><dt>设备数</dt><dd>{{ selectedSubscription.device_limit > 0 ? selectedSubscription.device_limit : '不限' }}</dd></div>
          </dl>
          <UiButton variant="secondary" type="button" @click="changeTarget">更换订阅</UiButton>
        </div>
        <div v-else-if="subscriptions.length" class="commerce-target-grid">
          <button
            v-for="subscription in subscriptions"
            :key="subscription.id"
            type="button"
            @click="selectTargetSubscription(subscription.id)"
          >
            <div>
              <span>订阅 #{{ subscription.id }}</span>
              <strong>{{ subscription.plan_name }}</strong>
              <small>{{ subscription.sku_name }}</small>
            </div>
            <UiIcon name="chevron" />
          </button>
        </div>
        <EmptyState
          v-else-if="subscriptionsLoaded"
          icon="plans"
          title="当前没有可操作的订阅"
          description="请先购买套餐。"
        >
          <template #actions><UiButton type="button" @click="returnToOverview">前往新购</UiButton></template>
        </EmptyState>
      </section>

      <section v-if="catalogVisible" class="commerce-hub-section" aria-labelledby="commerce-catalog-title">
        <div class="commerce-hub-heading">
          <div>
            <span>{{ currentOperation.label }}</span>
            <h2 id="commerce-catalog-title">{{ catalogTitle }}</h2>
            <p>{{ catalogDescription }}</p>
          </div>
        </div>

        <WorkbenchFilterBar
          v-if="operation === 'purchase' || operation === 'change'"
          :active="Boolean(query)"
          :loading="planLoading"
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

        <div v-if="planLoading" class="commerce-loading-state"><UiIcon name="refresh" />正在加载套餐</div>
        <div v-else-if="plans.length" class="commerce-catalog-grid commerce-account-catalog-grid">
          <CommercePlanCard
            v-for="plan in plans"
            :key="plan.id"
            :plan="plan"
            :offer="cardOfferState[plan.id]?.offer || null"
            :offer-count="cardOfferState[plan.id]?.total || 0"
            :loading="cardOfferState[plan.id]?.loading"
            :disabled="cardOfferState[plan.id]?.loading || !cardOfferState[plan.id]?.offer"
            @select="openPlanDetail(plan.id)"
          />
        </div>
        <EmptyState
          v-else-if="!planLoading"
          icon="plans"
          :title="query ? '没有找到匹配的套餐' : operation === 'change' ? '没有可切换的套餐' : '暂无可购买套餐'"
          :description="query ? '请尝试其他关键词。' : '可购买套餐发布后会显示在这里。'"
        />

        <TablePager
          v-if="planTotal > planLimit && (operation === 'purchase' || operation === 'change')"
          :total="planTotal"
          :offset="planOffset"
          :limit="planLimit"
          :loading="planLoading"
          :page-sizes="[6, 9, 12]"
          @change="changePlanPage"
        />
      </section>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  createOrder,
  fetchAccountSubscriptionsPage,
  fetchPlanCatalogItem,
  fetchPlanCatalogPage,
  fetchPlanCatalogSKUs,
  type AdminSubscriptionListItem,
  type PlanCatalogItem,
  type PlanSKU,
} from '../../api/client'
import CommercePlanCard from '../../components/CommercePlanCard.vue'
import CommercePlanDetail from '../../components/CommercePlanDetail.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageAlert from '../../components/PageAlert.vue'
import PageHeader from '../../components/PageHeader.vue'
import TablePager from '../../components/TablePager.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../../components/WorkbenchFilterInput.vue'
import { commerceErrorMessage } from '../../utils/commerceErrors'
import { formatBytes, formatCurrency } from '../../utils/format'

type PurchaseOperation = 'purchase' | 'renew' | 'change' | 'addon'

interface OperationOption {
  value: PurchaseOperation
  label: string
  title: string
  description: string
}

interface CardOfferState {
  loading: boolean
  offer: PlanSKU | null
  total: number
}

const operationOptions: OperationOption[] = [
  { value: 'purchase', label: '新购', title: '套餐中心', description: '选择套餐，进入详情比较规格并确认订单。' },
  { value: 'renew', label: '续费', title: '续费订阅', description: '选择续费规格并确认服务周期。' },
  { value: 'change', label: '切换套餐', title: '切换套餐', description: '为指定订阅选择新的套餐和规格。' },
  { value: 'addon', label: '流量包', title: '购买流量包', description: '为指定订阅增加可用流量。' },
]
const validOperations = new Set<PurchaseOperation>(operationOptions.map(item => item.value))
const skuTypeByOperation: Record<PurchaseOperation, PlanSKU['sku_type']> = {
  purchase: 'new',
  renew: 'renewal',
  change: 'upgrade',
  addon: 'traffic_pack',
}
const orderTypeByOperation: Record<PurchaseOperation, string> = {
  purchase: 'new',
  renew: 'renewal',
  change: 'upgrade',
  addon: 'traffic_pack',
}

const route = useRoute()
const router = useRouter()
const routeOperation = String(route.query.operation || 'purchase') as PurchaseOperation
const operation = ref<PurchaseOperation>(validOperations.has(routeOperation) ? routeOperation : 'purchase')
const targetSubscriptionID = ref(Math.max(0, Number(route.query.subscription) || 0))
const requestedPlanID = ref(Math.max(0, Number(route.query.plan) || 0))
const requestedSKUID = ref(Math.max(0, Number(route.query.sku) || 0))
const requestedStep = String(route.query.step || '')

const subscriptions = ref<AdminSubscriptionListItem[]>([])
const subscriptionsLoaded = ref(false)
const subscriptionLoading = ref(false)
const subscriptionError = ref('')

const plans = ref<PlanCatalogItem[]>([])
const planTotal = ref(0)
const planLoading = ref(false)
const planError = ref('')
const planOffset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * 9)
const planLimit = ref(9)
const query = ref(String(route.query.q || '').trim())
const searchDraft = ref(query.value)
const cardOfferState = reactive<Record<number, CardOfferState>>({})

const selectedPlan = ref<PlanCatalogItem | null>(null)
const detailSKUs = ref<PlanSKU[]>([])
const selectedSKUID = ref(requestedSKUID.value)
const detailLoading = ref(false)
const detailError = ref('')
const checkoutOpen = ref(false)
const creating = ref(false)
const actionError = ref('')
const checkoutError = ref('')

const currentOperation = computed(() => operationOptions.find(item => item.value === operation.value) || operationOptions[0]!)
const selectedSubscription = computed(() => subscriptions.value.find(item => item.id === targetSubscriptionID.value) || null)
const selectedSKU = computed(() => detailSKUs.value.find(item => item.id === selectedSKUID.value) || null)
const loading = computed(() => subscriptionLoading.value || planLoading.value || detailLoading.value || creating.value)
const catalogVisible = computed(() => operation.value === 'purchase' || Boolean(selectedSubscription.value))
const pageTitle = computed(() => checkoutOpen.value ? '确认订单' : selectedPlan.value ? selectedPlan.value.name : currentOperation.value.title)
const pageDescription = computed(() => checkoutOpen.value ? '核对订单内容后创建订单。' : selectedPlan.value ? '选择规格并继续结算。' : currentOperation.value.description)
const catalogTitle = computed(() => ({
  purchase: '购买新的订阅',
  renew: `${selectedSubscription.value?.plan_name || ''} 续费`,
  change: '选择新的套餐',
  addon: `${selectedSubscription.value?.plan_name || ''} 流量包`,
}[operation.value]))
const catalogDescription = computed(() => ({
  purchase: '选择套餐后进入商品详情。',
  renew: '选择可用的续费规格。',
  change: '选择要切换到的套餐。',
  addon: '选择需要增加的流量。',
}[operation.value]))

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

function formatDate(value: string) {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(value))
}

function clearCardOffers() {
  for (const key of Object.keys(cardOfferState)) delete cardOfferState[Number(key)]
}

async function syncURL(step = '') {
  const page = Math.floor(planOffset.value / planLimit.value) + 1
  const params: Record<string, string> = { operation: operation.value }
  if (targetSubscriptionID.value) params.subscription = String(targetSubscriptionID.value)
  if (query.value && (operation.value === 'purchase' || operation.value === 'change')) params.q = query.value
  if (page > 1 && !selectedPlan.value) params.page = String(page)
  if (selectedPlan.value) params.plan = String(selectedPlan.value.id)
  if (selectedSKUID.value) params.sku = String(selectedSKUID.value)
  if (step) params.step = step
  await router.replace({ query: params })
}

async function loadSubscriptions() {
  subscriptionLoading.value = true
  subscriptionError.value = ''
  try {
    const result = await fetchAccountSubscriptionsPage({ status: 'active', offset: 0, limit: 100 })
    subscriptions.value = result.items
    if (targetSubscriptionID.value && !subscriptions.value.some(item => item.id === targetSubscriptionID.value)) {
      targetSubscriptionID.value = 0
    }
  } catch (cause: any) {
    subscriptionError.value = cause?.response?.data?.message || '有效订阅加载失败。'
  } finally {
    subscriptionsLoaded.value = true
    subscriptionLoading.value = false
  }
}

async function loadCardOffer(plan: PlanCatalogItem) {
  cardOfferState[plan.id] = { loading: true, offer: null, total: 0 }
  try {
    const result = await fetchPlanCatalogSKUs(plan.id, {
      skuType: skuTypeByOperation[operation.value],
      offset: 0,
      limit: 1,
    })
    cardOfferState[plan.id] = {
      loading: false,
      offer: result.items[0] || null,
      total: result.total,
    }
  } catch {
    cardOfferState[plan.id] = { loading: false, offer: null, total: 0 }
  }
}

async function loadCatalog() {
  if (operation.value !== 'purchase' && !selectedSubscription.value) return
  planLoading.value = true
  planError.value = ''
  plans.value = []
  planTotal.value = 0
  clearCardOffers()
  try {
    if ((operation.value === 'renew' || operation.value === 'addon') && selectedSubscription.value) {
      const plan = await fetchPlanCatalogItem(selectedSubscription.value.plan_id)
      plans.value = [plan]
      planTotal.value = 1
    } else {
      const result = await fetchPlanCatalogPage({
        q: query.value || undefined,
        offset: planOffset.value,
        limit: planLimit.value,
      })
      if (operation.value === 'change') {
        const currentPlanID = selectedSubscription.value?.plan_id || 0
        plans.value = result.items.filter(plan => plan.id !== currentPlanID)
        planTotal.value = result.items.some(plan => plan.id === currentPlanID)
          ? Math.max(0, result.total - 1)
          : result.total
      } else {
        plans.value = result.items
        planTotal.value = result.total
      }
    }
    await Promise.all(plans.value.map(loadCardOffer))
  } catch (cause: any) {
    planError.value = cause?.response?.data?.message || '套餐目录加载失败。'
  } finally {
    planLoading.value = false
  }
}

async function openPlanDetail(planID: number, preserveRequestedSKU = false) {
  detailLoading.value = true
  detailError.value = ''
  actionError.value = ''
  checkoutError.value = ''
  try {
    const [plan, skuResult] = await Promise.all([
      fetchPlanCatalogItem(planID),
      fetchPlanCatalogSKUs(planID, {
        skuType: skuTypeByOperation[operation.value],
        offset: 0,
        limit: 100,
      }),
    ])
    selectedPlan.value = plan
    detailSKUs.value = skuResult.items
    const requested = preserveRequestedSKU
      ? skuResult.items.find(item => item.id === selectedSKUID.value)
      : undefined
    selectedSKUID.value = requested?.id || skuResult.items[0]?.id || 0
    checkoutOpen.value = false
    await syncURL('detail')
  } catch (cause: any) {
    detailError.value = cause?.response?.data?.message || '套餐详情加载失败。'
  } finally {
    detailLoading.value = false
  }
}

async function selectDetailSKU(sku: PlanSKU) {
  selectedSKUID.value = sku.id
  checkoutError.value = ''
  await syncURL('detail')
}

async function openCheckout() {
  if (!selectedSKU.value) return
  checkoutError.value = ''
  checkoutOpen.value = true
  await syncURL('checkout')
}

async function backToDetail() {
  checkoutError.value = ''
  checkoutOpen.value = false
  await syncURL('detail')
}

async function backToCatalog() {
  checkoutError.value = ''
  checkoutOpen.value = false
  selectedPlan.value = null
  detailSKUs.value = []
  selectedSKUID.value = 0
  requestedPlanID.value = 0
  requestedSKUID.value = 0
  await syncURL()
  if (!plans.value.length) await loadCatalog()
}

async function startOperation(next: Exclude<PurchaseOperation, 'purchase'>, subscriptionID: number) {
  operation.value = next
  targetSubscriptionID.value = subscriptionID
  query.value = ''
  searchDraft.value = ''
  planOffset.value = 0
  selectedPlan.value = null
  detailSKUs.value = []
  selectedSKUID.value = 0
  checkoutOpen.value = false
  checkoutError.value = ''
  await loadCatalog()
  if ((next === 'renew' || next === 'addon') && plans.value[0]) {
    await openPlanDetail(plans.value[0].id)
  } else {
    await syncURL()
  }
}

async function selectTargetSubscription(subscriptionID: number) {
  targetSubscriptionID.value = subscriptionID
  planOffset.value = 0
  checkoutError.value = ''
  await loadCatalog()
  await syncURL()
}

async function changeTarget() {
  targetSubscriptionID.value = 0
  plans.value = []
  planTotal.value = 0
  checkoutError.value = ''
  clearCardOffers()
  await syncURL()
}

async function returnToOverview() {
  operation.value = 'purchase'
  targetSubscriptionID.value = 0
  query.value = ''
  searchDraft.value = ''
  planOffset.value = 0
  selectedPlan.value = null
  detailSKUs.value = []
  selectedSKUID.value = 0
  checkoutOpen.value = false
  checkoutError.value = ''
  await loadCatalog()
  await syncURL()
}

async function submitSearch() {
  query.value = searchDraft.value.trim()
  searchDraft.value = query.value
  planOffset.value = 0
  await loadCatalog()
  await syncURL()
}

async function clearSearch() {
  query.value = ''
  searchDraft.value = ''
  planOffset.value = 0
  await loadCatalog()
  await syncURL()
}

async function changePlanPage(value: { offset: number; limit: number }) {
  planOffset.value = value.offset
  planLimit.value = value.limit
  await loadCatalog()
  await syncURL()
}

async function refreshAll() {
  actionError.value = ''
  checkoutError.value = ''
  await loadSubscriptions()
  await loadCatalog()
}

async function submitOrder() {
  if (!selectedPlan.value || !selectedSKU.value) return
  if (operation.value !== 'purchase' && !selectedSubscription.value) {
    checkoutError.value = '请选择目标订阅后再创建订单。'
    return
  }
  creating.value = true
  checkoutError.value = ''
  try {
    await createOrder(selectedSKU.value.id, {
      orderType: orderTypeByOperation[operation.value],
      targetSubscriptionId: operation.value === 'purchase' ? undefined : selectedSubscription.value?.id,
    })
    await router.push('/account/orders')
  } catch (cause: any) {
    checkoutError.value = commerceErrorMessage(cause, '订单创建失败，请检查当前套餐和规格是否仍可购买。')
  } finally {
    creating.value = false
  }
}

watch(
  () => [route.query.q, route.query.page],
  async () => {
    if (selectedPlan.value || checkoutOpen.value) return
    const nextQuery = String(route.query.q || '').trim()
    const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * planLimit.value
    if (query.value === nextQuery && planOffset.value === nextOffset) return
    query.value = nextQuery
    searchDraft.value = nextQuery
    planOffset.value = nextOffset
    await loadCatalog()
  },
)

onMounted(async () => {
  await loadSubscriptions()

  if (operation.value !== 'purchase' && !selectedSubscription.value) {
    await syncURL()
    return
  }

  if (requestedPlanID.value) {
    await openPlanDetail(requestedPlanID.value, true)
    if (requestedStep === 'checkout' && selectedSKU.value) {
      checkoutOpen.value = true
      await syncURL('checkout')
    }
    return
  }

  await loadCatalog()
  await syncURL()
})
</script>
