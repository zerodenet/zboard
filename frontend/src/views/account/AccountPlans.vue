<template>
  <section class="account-page commerce-account-page commerce-hub-page stack">
    <PageHeader
      :title="pageTitle"
      :description="pageDescription"
      eyebrow="PLANS"
    >
      <template #actions>
        <UiButton v-if="operation !== 'purchase'" variant="secondary" type="button" @click="returnToOverview">
          返回套餐中心
        </UiButton>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="refreshAll">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <PageAlert v-if="actionError" tone="danger" title="套餐操作失败">{{ actionError }}</PageAlert>
    <PageAlert v-if="subscriptionError" tone="danger" title="订阅加载失败">{{ subscriptionError }}</PageAlert>
    <PageAlert v-if="planError" tone="danger" title="套餐目录加载失败">{{ planError }}</PageAlert>

    <section v-if="operation === 'purchase'" class="commerce-hub-section" aria-labelledby="active-subscriptions-title">
      <div class="commerce-hub-heading">
        <div>
          <span>当前服务</span>
          <h2 id="active-subscriptions-title">管理现有订阅</h2>
          <p>续费、切换套餐和购买流量包都从具体订阅发起，不再混入新购流程。</p>
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
        description="从下方选择套餐和计费周期即可创建新的订阅。"
      />
    </section>

    <section v-else class="commerce-hub-section" aria-labelledby="target-subscription-title">
      <div class="commerce-hub-heading">
        <div>
          <span>操作对象</span>
          <h2 id="target-subscription-title">{{ selectedSubscription ? '确认目标订阅' : '选择目标订阅' }}</h2>
          <p>{{ selectedSubscription ? '本次操作只会作用于这一个订阅。' : '先选择需要处理的订阅，再进入对应商品目录。' }}</p>
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
          <div><dt>当前设备数</dt><dd>{{ selectedSubscription.device_limit > 0 ? selectedSubscription.device_limit : '不限' }}</dd></div>
        </dl>
        <UiButton variant="secondary" type="button" @click="changeTarget">更换目标订阅</UiButton>
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
        description="请先完成一次新购，之后才能续费、切换套餐或购买流量包。"
      >
        <template #actions><UiButton type="button" @click="returnToOverview">前往新购</UiButton></template>
      </EmptyState>
    </section>

    <section v-if="catalogVisible" class="commerce-hub-section" aria-labelledby="commerce-catalog-title">
      <div class="commerce-hub-heading">
        <div>
          <span>{{ operation === 'purchase' ? '购买新套餐' : currentOperation.label }}</span>
          <h2 id="commerce-catalog-title">{{ catalogTitle }}</h2>
          <p>{{ catalogDescription }}</p>
        </div>
      </div>

      <div v-if="planLoading" class="commerce-loading-state"><UiIcon name="refresh" />正在加载套餐和价格</div>
      <div v-else-if="plans.length" class="commerce-catalog-grid commerce-account-catalog-grid">
        <article v-for="plan in plans" :key="plan.id" class="commerce-product-card">
          <header class="commerce-product-head">
            <div>
              <span>{{ plan.slug }}</span>
              <h3>{{ plan.name }}</h3>
            </div>
            <small v-if="offersFor(plan.id).length">{{ offersFor(plan.id).length }} 个可用规格</small>
          </header>

          <p class="commerce-product-summary">{{ plan.summary || '稳定、透明的订阅服务。' }}</p>

          <div v-if="offerState[plan.id]?.loading" class="commerce-card-loading"><UiIcon name="refresh" />正在加载价格</div>
          <template v-else-if="selectedOffer(plan.id)">
            <div class="commerce-product-price">
              <strong>{{ formatCurrency(selectedOffer(plan.id)!.price_cents, selectedOffer(plan.id)!.currency) }}</strong>
              <span>/ {{ billingLabel(selectedOffer(plan.id)!) }}</span>
            </div>
            <div class="commerce-offer-tabs" role="radiogroup" :aria-label="`${plan.name} 可用规格`">
              <button
                v-for="sku in offersFor(plan.id)"
                :key="sku.id"
                type="button"
                role="radio"
                :aria-checked="selectedOfferIDs[plan.id] === sku.id"
                :class="{ active: selectedOfferIDs[plan.id] === sku.id }"
                @click="selectOffer(plan, sku)"
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
            <strong>当前场景不可购买</strong>
            <span>管理员尚未配置对应销售规格</span>
          </div>

          <ul v-if="operation !== 'addon'" class="commerce-entitlement-list">
            <li><UiIcon name="check" />{{ formatBytes(plan.traffic_bytes) }} 套餐流量</li>
            <li><UiIcon name="check" />{{ plan.device_limit > 0 ? `${plan.device_limit} 台设备` : '不限设备' }}</li>
            <li><UiIcon name="check" />{{ plan.speed_limit_mbps ? `${plan.speed_limit_mbps} Mbps` : '不限速' }}</li>
          </ul>
          <ul v-else-if="selectedOffer(plan.id)" class="commerce-entitlement-list">
            <li><UiIcon name="check" />增加 {{ formatBytes(selectedOffer(plan.id)!.grant_traffic_bytes) }} 可用流量</li>
            <li><UiIcon name="check" />保持当前设备数和限速</li>
            <li><UiIcon name="check" />不改变订阅到期时间</li>
          </ul>

          <div class="commerce-card-footer">
            <UiButton type="button" :disabled="!selectedOffer(plan.id)" @click="openCheckout(plan)">
              继续结算<UiIcon name="chevron" />
            </UiButton>
          </div>
        </article>
      </div>
      <EmptyState
        v-else-if="!planLoading"
        icon="plans"
        :title="operation === 'change' ? '没有可切换的套餐' : '暂无可购买套餐'"
        description="管理员发布对应商品和销售规格后会显示在这里。"
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

    <ModalDialog
      :open="checkoutOpen"
      :title="`确认${currentOperation.label}订单`"
      description="请核对套餐、计费周期、权益和应付金额。"
      size="lg"
      :busy="creating"
      @close="checkoutOpen = false"
    >
      <div v-if="selectedPlan && selectedSKU" class="commerce-checkout-preview">
        <section class="checkout-product-summary">
          <div>
            <span>{{ currentOperation.label }}</span>
            <h3>{{ selectedPlan.name }}</h3>
            <p>{{ selectedSKU.name }} · {{ billingLabel(selectedSKU) }}</p>
          </div>
          <strong>{{ formatCurrency(selectedSKU.price_cents, selectedSKU.currency) }}</strong>
        </section>

        <dl class="checkout-detail-list">
          <div v-if="selectedSubscription"><dt>目标订阅</dt><dd>{{ selectedSubscription.plan_name }} / #{{ selectedSubscription.id }}</dd></div>
          <div><dt>订单操作</dt><dd>{{ currentOperation.label }}</dd></div>
          <div><dt>计费规格</dt><dd>{{ selectedSKU.name }}</dd></div>
          <div><dt>服务周期</dt><dd>{{ billingLabel(selectedSKU) }}</dd></div>
          <template v-if="operation === 'addon'">
            <div><dt>附加流量</dt><dd>{{ formatBytes(selectedSKU.grant_traffic_bytes) }}</dd></div>
            <div><dt>其他权益</dt><dd>保持目标订阅现状</dd></div>
          </template>
          <template v-else>
            <div><dt>套餐流量</dt><dd>{{ formatBytes(selectedPlan.traffic_bytes) }}</dd></div>
            <div><dt>设备数</dt><dd>{{ selectedPlan.device_limit > 0 ? selectedPlan.device_limit : '不限' }}</dd></div>
            <div><dt>速率限制</dt><dd>{{ selectedPlan.speed_limit_mbps ? `${selectedPlan.speed_limit_mbps} Mbps` : '不限速' }}</dd></div>
          </template>
        </dl>

        <PageAlert tone="info" title="订单快照">
          确认后，当前价格、周期和套餐权益会写入订单快照；商品后续调整不会修改这笔订单。
        </PageAlert>
      </div>
      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="creating" @click="requestClose">返回修改</UiButton>
        <UiButton type="button" :loading="creating" @click="submitOrder">确认创建订单</UiButton>
      </template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
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
import EmptyState from '../../components/EmptyState.vue'
import ModalDialog from '../../components/ModalDialog.vue'
import PageAlert from '../../components/PageAlert.vue'
import PageHeader from '../../components/PageHeader.vue'
import TablePager from '../../components/TablePager.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import { formatBytes, formatCurrency } from '../../utils/format'

type PurchaseOperation = 'purchase' | 'renew' | 'change' | 'addon'

interface OperationOption {
  value: PurchaseOperation
  label: string
  title: string
  description: string
}

interface OfferState {
  loading: boolean
  error: string
  items: PlanSKU[]
}

const operationOptions: OperationOption[] = [
  { value: 'purchase', label: '新购', title: '选择套餐', description: '比较套餐权益和计费周期，确认后创建新的独立订阅。' },
  { value: 'renew', label: '续费', title: '续费订阅', description: '为指定订阅选择续费周期，并按订单快照延长服务期限。' },
  { value: 'change', label: '切换套餐', title: '切换套餐', description: '为指定订阅选择其他套餐，确认新的权益和计费周期。' },
  { value: 'addon', label: '流量包', title: '购买流量包', description: '为指定订阅增加一次性流量，不改变设备数、限速和到期时间。' },
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

const subscriptions = ref<AdminSubscriptionListItem[]>([])
const subscriptionsLoaded = ref(false)
const subscriptionLoading = ref(false)
const subscriptionError = ref('')
const plans = ref<PlanCatalogItem[]>([])
const planTotal = ref(0)
const planLoading = ref(false)
const planError = ref('')
const planOffset = ref(0)
const planLimit = ref(9)
const offerState = reactive<Record<number, OfferState>>({})
const selectedOfferIDs = reactive<Record<number, number>>({})
const selectedPlan = ref<PlanCatalogItem | null>(null)
const selectedSKU = ref<PlanSKU | null>(null)
const checkoutOpen = ref(false)
const creating = ref(false)
const actionError = ref('')

const currentOperation = computed(() => operationOptions.find(item => item.value === operation.value) || operationOptions[0]!)
const selectedSubscription = computed(() => subscriptions.value.find(item => item.id === targetSubscriptionID.value) || null)
const pageTitle = computed(() => currentOperation.value.title)
const pageDescription = computed(() => currentOperation.value.description)
const loading = computed(() => subscriptionLoading.value || planLoading.value || creating.value)
const catalogVisible = computed(() => operation.value === 'purchase' || Boolean(selectedSubscription.value))
const catalogTitle = computed(() => ({
  purchase: '购买新的独立订阅',
  renew: `选择 ${selectedSubscription.value?.plan_name || ''} 的续费周期`,
  change: '选择要切换到的套餐',
  addon: `选择 ${selectedSubscription.value?.plan_name || ''} 的流量包`,
}[operation.value]))
const catalogDescription = computed(() => ({
  purchase: '每张卡片直接展示套餐权益和全部可购买周期。',
  renew: '续费只显示当前商品允许续费的周期规格。',
  change: '当前套餐已排除，只展示允许切换的其他商品。',
  addon: '流量包只增加可用流量，其他订阅权益保持不变。',
}[operation.value]))

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

function formatDate(value: string) {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(value))
}

function offersFor(planID: number) {
  return offerState[planID]?.items || []
}

function selectedOffer(planID: number) {
  const offers = offersFor(planID)
  return offers.find(item => item.id === selectedOfferIDs[planID]) || offers[0] || null
}

function clearOffers() {
  for (const key of Object.keys(offerState)) delete offerState[Number(key)]
  for (const key of Object.keys(selectedOfferIDs)) delete selectedOfferIDs[Number(key)]
}

async function syncURL() {
  const page = Math.floor(planOffset.value / planLimit.value) + 1
  const query: Record<string, string> = { operation: operation.value }
  if (targetSubscriptionID.value) query.subscription = String(targetSubscriptionID.value)
  if (page > 1) query.page = String(page)
  if (selectedPlan.value) query.plan = String(selectedPlan.value.id)
  if (selectedSKU.value) query.sku = String(selectedSKU.value.id)
  await router.replace({ query })
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

async function loadOffers(plan: PlanCatalogItem) {
  offerState[plan.id] = { loading: true, error: '', items: [] }
  try {
    const result = await fetchPlanCatalogSKUs(plan.id, {
      skuType: skuTypeByOperation[operation.value],
      offset: 0,
      limit: 100,
    })
    offerState[plan.id] = { loading: false, error: '', items: result.items }
    const requested = plan.id === requestedPlanID.value
      ? result.items.find(item => item.id === requestedSKUID.value)
      : undefined
    selectedOfferIDs[plan.id] = requested?.id || result.items[0]?.id || 0
    if (requested) {
      selectedPlan.value = plan
      selectedSKU.value = requested
    }
  } catch (cause: any) {
    offerState[plan.id] = {
      loading: false,
      error: cause?.response?.data?.message || '销售规格加载失败。',
      items: [],
    }
    selectedOfferIDs[plan.id] = 0
  }
}

async function loadPlans() {
  planLoading.value = true
  planError.value = ''
  plans.value = []
  planTotal.value = 0
  selectedPlan.value = null
  selectedSKU.value = null
  clearOffers()
  try {
    if (operation.value !== 'purchase' && !selectedSubscription.value) return

    if ((operation.value === 'renew' || operation.value === 'addon') && selectedSubscription.value) {
      const plan = await fetchPlanCatalogItem(selectedSubscription.value.plan_id)
      plans.value = [plan]
      planTotal.value = 1
    } else {
      const result = await fetchPlanCatalogPage({ offset: planOffset.value, limit: planLimit.value })
      if (operation.value === 'change') {
        const currentPlanID = selectedSubscription.value?.plan_id || 0
        plans.value = result.items.filter(plan => plan.id !== currentPlanID)
        planTotal.value = result.items.some(plan => plan.id === currentPlanID) ? Math.max(0, result.total - 1) : result.total
      } else {
        plans.value = result.items
        planTotal.value = result.total
      }
    }
    await Promise.all(plans.value.map(loadOffers))
  } catch (cause: any) {
    planError.value = cause?.response?.data?.message || '套餐目录加载失败。'
  } finally {
    planLoading.value = false
  }
}

async function startOperation(next: Exclude<PurchaseOperation, 'purchase'>, subscriptionID: number) {
  operation.value = next
  targetSubscriptionID.value = subscriptionID
  requestedPlanID.value = 0
  requestedSKUID.value = 0
  planOffset.value = 0
  actionError.value = ''
  await loadPlans()
  await syncURL()
}

async function selectTargetSubscription(subscriptionID: number) {
  targetSubscriptionID.value = subscriptionID
  requestedPlanID.value = 0
  requestedSKUID.value = 0
  await loadPlans()
  await syncURL()
}

async function changeTarget() {
  targetSubscriptionID.value = 0
  plans.value = []
  selectedPlan.value = null
  selectedSKU.value = null
  clearOffers()
  await syncURL()
}

async function returnToOverview() {
  operation.value = 'purchase'
  targetSubscriptionID.value = 0
  requestedPlanID.value = 0
  requestedSKUID.value = 0
  planOffset.value = 0
  actionError.value = ''
  await loadPlans()
  await syncURL()
}

async function selectOffer(plan: PlanCatalogItem, sku: PlanSKU) {
  selectedOfferIDs[plan.id] = sku.id
  selectedPlan.value = plan
  selectedSKU.value = sku
  requestedPlanID.value = plan.id
  requestedSKUID.value = sku.id
  await syncURL()
}

async function openCheckout(plan: PlanCatalogItem) {
  const sku = selectedOffer(plan.id)
  if (!sku) return
  selectedPlan.value = plan
  selectedSKU.value = sku
  requestedPlanID.value = plan.id
  requestedSKUID.value = sku.id
  checkoutOpen.value = true
  await syncURL()
}

async function changePlanPage(value: { offset: number; limit: number }) {
  planOffset.value = value.offset
  planLimit.value = value.limit
  requestedPlanID.value = 0
  requestedSKUID.value = 0
  await loadPlans()
  await syncURL()
}

async function refreshAll() {
  actionError.value = ''
  await loadSubscriptions()
  await loadPlans()
}

async function submitOrder() {
  if (!selectedPlan.value || !selectedSKU.value) return
  if (operation.value !== 'purchase' && !selectedSubscription.value) {
    actionError.value = '请选择目标订阅。'
    checkoutOpen.value = false
    return
  }
  creating.value = true
  actionError.value = ''
  try {
    await createOrder(selectedSKU.value.id, {
      orderType: orderTypeByOperation[operation.value],
      targetSubscriptionId: operation.value === 'purchase' ? undefined : selectedSubscription.value?.id,
    })
    checkoutOpen.value = false
    await router.push('/account/orders')
  } catch (cause: any) {
    actionError.value = cause?.response?.data?.message || '订单创建失败。'
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  const page = Math.max(1, Number(route.query.page) || 1)
  planOffset.value = (page - 1) * planLimit.value
  await loadSubscriptions()
  await loadPlans()
  await syncURL()
})
</script>
