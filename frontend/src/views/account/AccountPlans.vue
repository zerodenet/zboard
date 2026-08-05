<template>
  <section class="account-page commerce-account-page stack">
    <PageHeader
      title="购买与管理套餐"
      description="选择购买方式、目标订阅、套餐和计费周期，最后在结算预览中确认订单。"
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
      :error="pageError"
      success-title="订单已创建"
      error-title="套餐操作失败"
    />

    <section class="commerce-mode-panel" aria-labelledby="commerce-mode-title">
      <div class="commerce-mode-copy">
        <span>第一步</span>
        <h2 id="commerce-mode-title">这次要做什么？</h2>
        <p>{{ currentOperation.description }}</p>
      </div>
      <div class="commerce-mode-tabs" role="tablist" aria-label="购买方式">
        <button
          v-for="item in operationOptions"
          :key="item.value"
          type="button"
          role="tab"
          :aria-selected="operation === item.value"
          :disabled="item.value !== 'purchase' && subscriptionsLoaded && !subscriptions.length"
          :class="{ active: operation === item.value }"
          @click="setOperation(item.value)"
        >
          <UiIcon :name="item.icon" />
          <span><strong>{{ item.label }}</strong><small>{{ item.short }}</small></span>
        </button>
      </div>
    </section>

    <PageAlert v-if="subscriptionError" tone="danger" title="订阅加载失败">{{ subscriptionError }}</PageAlert>

    <section v-if="operation !== 'purchase'" class="commerce-step-panel" aria-labelledby="target-subscription-title">
      <div class="commerce-section-heading">
        <div>
          <span>第二步</span>
          <h2 id="target-subscription-title">选择目标订阅</h2>
          <p>续费、套餐切换和流量包必须明确作用于哪一个现有订阅。</p>
        </div>
        <StatusBadge v-if="subscriptions.length" tone="neutral">{{ subscriptions.length }} 个有效订阅</StatusBadge>
      </div>

      <div v-if="subscriptions.length" class="commerce-subscription-grid">
        <button
          v-for="subscription in subscriptions"
          :key="subscription.id"
          type="button"
          :class="{ selected: targetSubscriptionID === subscription.id }"
          @click="selectSubscription(subscription.id)"
        >
          <span class="commerce-selection-marker"><UiIcon name="check" /></span>
          <div>
            <strong>{{ subscription.plan_name }}</strong>
            <small>{{ subscription.sku_name }} · 订阅 #{{ subscription.id }}</small>
          </div>
          <dl>
            <div><dt>剩余流量</dt><dd>{{ formatBytes(Math.max(0, subscription.flow_total - subscription.flow_used)) }}</dd></div>
            <div><dt>到期时间</dt><dd>{{ formatDate(subscription.end_at) }}</dd></div>
          </dl>
        </button>
      </div>
      <EmptyState
        v-else-if="subscriptionsLoaded"
        icon="plans"
        title="当前没有可操作的订阅"
        description="请先完成一次新购，之后才能续费、切换套餐或购买流量包。"
      >
        <template #actions><UiButton type="button" @click="setOperation('purchase')">前往新购</UiButton></template>
      </EmptyState>
    </section>

    <section class="commerce-step-panel" aria-labelledby="plan-catalog-title">
      <div class="commerce-section-heading">
        <div>
          <span>{{ operation === 'purchase' ? '第二步' : '第三步' }}</span>
          <h2 id="plan-catalog-title">{{ planStepTitle }}</h2>
          <p>{{ planStepDescription }}</p>
        </div>
      </div>

      <WorkbenchFilterBar
        v-if="operation === 'purchase' || operation === 'change'"
        :active="Boolean(planQuery || planDraftQuery)"
        @clear="clearPlanFilters"
      >
        <WorkbenchFilterInput v-model="planDraftQuery" label="搜索" placeholder="套餐名称或简介" @apply="applyPlanFilters" />
      </WorkbenchFilterBar>

      <PageAlert v-if="planError" tone="danger" title="套餐目录加载失败">{{ planError }}</PageAlert>
      <div v-if="planLoading" class="commerce-loading" role="status"><UiIcon name="refresh" />正在加载套餐</div>
      <div v-else-if="plans.length" class="account-commerce-plan-grid">
        <article
          v-for="plan in plans"
          :key="plan.id"
          :class="{ selected: selectedPlanID === plan.id }"
        >
          <span v-if="selectedPlanID === plan.id" class="commerce-selection-marker"><UiIcon name="check" /></span>
          <header>
            <div>
              <small>{{ plan.slug }}</small>
              <h3>{{ plan.name }}</h3>
            </div>
            <StatusBadge v-if="selectedPlanID === plan.id" tone="success">已选择</StatusBadge>
          </header>
          <p>{{ plan.summary || '稳定、透明的订阅服务。' }}</p>
          <div v-if="plan.primary_sku" class="commerce-plan-price">
            <strong>{{ formatCurrency(plan.primary_sku.price_cents, plan.primary_sku.currency) }}</strong>
            <span>起 / {{ billingLabel(plan.primary_sku) }}</span>
          </div>
          <ul>
            <li><UiIcon name="check" />{{ formatBytes(plan.traffic_bytes) }} 套餐流量</li>
            <li><UiIcon name="check" />{{ plan.device_limit || '不限' }} 台设备</li>
            <li><UiIcon name="check" />{{ plan.speed_limit_mbps ? `${plan.speed_limit_mbps} Mbps` : '不限速' }}</li>
          </ul>
          <UiButton
            :variant="selectedPlanID === plan.id ? 'primary' : 'secondary'"
            type="button"
            @click="selectPlan(plan)"
          >
            {{ selectedPlanID === plan.id ? '重新加载计费周期' : selectPlanLabel }}
          </UiButton>
        </article>
      </div>
      <EmptyState
        v-else-if="!planLoading"
        icon="plans"
        :title="planQuery ? '没有匹配的套餐' : emptyPlanTitle"
        :description="emptyPlanDescription"
      />

      <TablePager
        v-if="(operation === 'purchase' || operation === 'change') && planTotal > planLimit"
        :total="planTotal"
        :offset="planOffset"
        :limit="planLimit"
        :loading="planLoading"
        :page-sizes="[6, 12, 24]"
        @change="changePlanPage"
      />
    </section>

    <section v-if="selectedPlan" class="commerce-step-panel commerce-offer-panel" aria-labelledby="offer-title">
      <div class="commerce-section-heading">
        <div>
          <span>{{ operation === 'purchase' ? '第三步' : '第四步' }}</span>
          <h2 id="offer-title">选择 {{ selectedPlan.name }} 的计费周期</h2>
          <p>所有周期规格共享同一组套餐权益，仅价格和服务期限不同。</p>
        </div>
        <StatusBadge :tone="skus.length ? 'success' : 'warning'">{{ skus.length }} 个可用规格</StatusBadge>
      </div>

      <PageAlert v-if="skuError" tone="danger" title="销售规格加载失败">{{ skuError }}</PageAlert>
      <div v-if="skuLoading" class="commerce-loading" role="status"><UiIcon name="refresh" />正在加载计费周期</div>
      <div v-else-if="skus.length" class="commerce-sku-choice-grid">
        <button
          v-for="sku in skus"
          :key="sku.id"
          type="button"
          :class="{ selected: selectedSKU?.id === sku.id }"
          @click="selectSKU(sku)"
        >
          <span class="commerce-selection-marker"><UiIcon name="check" /></span>
          <header>
            <div><strong>{{ sku.name }}</strong><small>{{ sku.code }}</small></div>
            <StatusBadge v-if="selectedSKU?.id === sku.id" tone="success">已选择</StatusBadge>
          </header>
          <div class="commerce-sku-price">
            <strong>{{ formatCurrency(sku.price_cents, sku.currency) }}</strong>
            <span>{{ billingLabel(sku) }}</span>
          </div>
          <p v-if="operation === 'addon'">增加 {{ formatBytes(sku.grant_traffic_bytes) }} 可用流量</p>
          <p v-else>继承 {{ selectedPlan.name }} 的全部套餐权益</p>
        </button>
      </div>
      <EmptyState v-else-if="!skuLoading" icon="plans" title="当前场景没有可用规格" description="管理员需要为该商品启用对应的购买场景。" />

      <div v-if="selectedSKU" class="commerce-order-bar">
        <div>
          <span>{{ currentOperation.label }} · {{ selectedPlan.name }}</span>
          <strong>{{ selectedSKU.name }}，{{ formatCurrency(selectedSKU.price_cents, selectedSKU.currency) }}</strong>
        </div>
        <UiButton type="button" @click="checkoutOpen = true">查看并确认订单<UiIcon name="arrow-right" /></UiButton>
      </div>
    </section>

    <ModalDialog
      :open="checkoutOpen"
      :title="`确认${currentOperation.label}订单`"
      description="请核对操作对象、套餐权益、计费周期和应付金额。"
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
          <div><dt>计费周期</dt><dd>{{ billingLabel(selectedSKU) }}</dd></div>
          <template v-if="operation === 'addon'">
            <div><dt>附加流量</dt><dd>{{ formatBytes(selectedSKU.grant_traffic_bytes) }}</dd></div>
            <div><dt>其他权益</dt><dd>保持目标订阅现状</dd></div>
          </template>
          <template v-else>
            <div><dt>套餐流量</dt><dd>{{ formatBytes(selectedPlan.traffic_bytes) }}</dd></div>
            <div><dt>设备数</dt><dd>{{ selectedPlan.device_limit || '不限' }}</dd></div>
            <div><dt>速率限制</dt><dd>{{ selectedPlan.speed_limit_mbps ? `${selectedPlan.speed_limit_mbps} Mbps` : '不限速' }}</dd></div>
          </template>
          <div><dt>支付方式</dt><dd>创建订单后等待支付或人工确认</dd></div>
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
import { computed, onMounted, ref } from 'vue'
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
import StatusBadge from '../../components/StatusBadge.vue'
import TablePager from '../../components/TablePager.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../../components/WorkbenchFilterInput.vue'
import { formatBytes, formatCurrency } from '../../utils/format'

type PurchaseOperation = 'purchase' | 'renew' | 'change' | 'addon'

const operationOptions: Array<{ value: PurchaseOperation; label: string; short: string; description: string; icon: string }> = [
  { value: 'purchase', label: '新购', short: '创建新的独立订阅', description: '选择一个商品和计费周期，创建全新的订阅订单。', icon: 'plus' },
  { value: 'renew', label: '续费', short: '延长当前订阅期限', description: '为现有订阅延长服务周期，并按订单快照刷新套餐权益。', icon: 'refresh' },
  { value: 'change', label: '切换套餐', short: '迁移到其他商品', description: '选择目标订阅和新的商品，确认后创建套餐切换订单。', icon: 'plans' },
  { value: 'addon', label: '流量包', short: '仅增加可用流量', description: '为目标订阅增加一次性流量，不修改限速、设备数或服务周期。', icon: 'traffic' },
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
const selectedPlanID = ref(Math.max(0, Number(route.query.plan) || 0))
const selectedPlan = ref<PlanCatalogItem | null>(null)
const selectedSKU = ref<PlanSKU | null>(null)
const requestedSKUID = ref(Math.max(0, Number(route.query.sku) || 0))

const subscriptions = ref<AdminSubscriptionListItem[]>([])
const subscriptionsLoaded = ref(false)
const subscriptionLoading = ref(false)
const subscriptionError = ref('')
const plans = ref<PlanCatalogItem[]>([])
const planTotal = ref(0)
const planLoading = ref(false)
const planError = ref('')
const planQuery = ref(String(route.query.q || '').trim())
const planDraftQuery = ref(planQuery.value)
const allowedPageSizes = [6, 12, 24]
const routeLimit = Number(route.query.limit)
const planLimit = ref(allowedPageSizes.includes(routeLimit) ? routeLimit : 6)
const planOffset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * planLimit.value)
const skus = ref<PlanSKU[]>([])
const skuLoading = ref(false)
const skuError = ref('')
const creating = ref(false)
const checkoutOpen = ref(false)
const message = ref('')
const actionError = ref('')

const currentOperation = computed(() => operationOptions.find(item => item.value === operation.value) || operationOptions[0])
const selectedSubscription = computed(() => subscriptions.value.find(item => item.id === targetSubscriptionID.value) || null)
const loading = computed(() => subscriptionLoading.value || planLoading.value || skuLoading.value || creating.value)
const pageError = computed(() => actionError.value)
const planStepTitle = computed(() => ({
  purchase: '选择一个套餐',
  renew: '确认续费商品',
  change: '选择要切换到的套餐',
  addon: '选择流量包所属商品',
}[operation.value]))
const planStepDescription = computed(() => ({
  purchase: '套餐决定流量、设备数和限速，计费周期将在下一步选择。',
  renew: '续费必须使用目标订阅当前所属的商品。',
  change: '当前订阅所属商品会被排除，只显示可切换的其他套餐。',
  addon: '流量包只作用于目标订阅，其他权益保持不变。',
}[operation.value]))
const selectPlanLabel = computed(() => operation.value === 'change' ? '切换到此套餐' : '选择此套餐')
const emptyPlanTitle = computed(() => operation.value === 'change' ? '没有可切换的套餐' : '暂无可用套餐')
const emptyPlanDescription = computed(() => operation.value === 'change'
  ? '当前目录中没有其他可用于套餐切换的商品。'
  : '管理员发布商品和对应销售规格后会显示在这里。')

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}

function formatDate(value: string) {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(value))
}

async function syncURL(replace = true) {
  const page = Math.floor(planOffset.value / planLimit.value) + 1
  const query: Record<string, string> = { operation: operation.value }
  if (targetSubscriptionID.value) query.subscription = String(targetSubscriptionID.value)
  if (planQuery.value) query.q = planQuery.value
  if (page > 1) query.page = String(page)
  if (planLimit.value !== 6) query.limit = String(planLimit.value)
  if (selectedPlanID.value) query.plan = String(selectedPlanID.value)
  if (selectedSKU.value?.id) query.sku = String(selectedSKU.value.id)
  await (replace ? router.replace({ query }) : router.push({ query }))
}

async function loadSubscriptions() {
  subscriptionLoading.value = true
  subscriptionError.value = ''
  try {
    const result = await fetchAccountSubscriptionsPage({ status: 'active', offset: 0, limit: 100 })
    subscriptions.value = result.items
    if (operation.value !== 'purchase') {
      if (!subscriptions.value.some(item => item.id === targetSubscriptionID.value)) {
        targetSubscriptionID.value = subscriptions.value[0]?.id || 0
      }
    }
  } catch (cause: any) {
    subscriptionError.value = cause?.response?.data?.message || '有效订阅加载失败。'
  } finally {
    subscriptionsLoaded.value = true
    subscriptionLoading.value = false
  }
}

async function loadPlans() {
  planLoading.value = true
  planError.value = ''
  selectedPlan.value = null
  selectedPlanID.value = 0
  selectedSKU.value = null
  skus.value = []
  try {
    if ((operation.value === 'renew' || operation.value === 'addon') && selectedSubscription.value) {
      const plan = await fetchPlanCatalogItem(selectedSubscription.value.plan_id)
      plans.value = [plan]
      planTotal.value = 1
      await selectPlan(plan, false)
      return
    }
    const result = await fetchPlanCatalogPage({
      q: planQuery.value || undefined,
      offset: planOffset.value,
      limit: planLimit.value,
    })
    const currentPlanID = selectedSubscription.value?.plan_id || 0
    plans.value = operation.value === 'change'
      ? result.items.filter(plan => plan.id !== currentPlanID)
      : result.items
    planTotal.value = operation.value === 'change' && result.items.some(plan => plan.id === currentPlanID)
      ? Math.max(0, result.total - 1)
      : result.total
    const requestedPlan = plans.value.find(plan => plan.id === Number(route.query.plan))
    if (requestedPlan) await selectPlan(requestedPlan, false)
  } catch (cause: any) {
    planError.value = cause?.response?.data?.message || '套餐目录加载失败。'
  } finally {
    planLoading.value = false
  }
}

async function loadSKUs() {
  if (!selectedPlan.value) return
  skuLoading.value = true
  skuError.value = ''
  selectedSKU.value = null
  try {
    const result = await fetchPlanCatalogSKUs(selectedPlan.value.id, {
      skuType: skuTypeByOperation[operation.value],
      offset: 0,
      limit: 100,
    })
    skus.value = result.items
    const requested = skus.value.find(sku => sku.id === requestedSKUID.value)
    if (requested) selectedSKU.value = requested
  } catch (cause: any) {
    skuError.value = cause?.response?.data?.message || '销售规格加载失败。'
  } finally {
    skuLoading.value = false
  }
}

async function setOperation(next: PurchaseOperation) {
  if (next !== 'purchase' && subscriptionsLoaded.value && !subscriptions.value.length) return
  operation.value = next
  actionError.value = ''
  message.value = ''
  planQuery.value = ''
  planDraftQuery.value = ''
  planOffset.value = 0
  requestedSKUID.value = 0
  if (next === 'purchase') {
    targetSubscriptionID.value = 0
  } else if (!subscriptions.value.some(item => item.id === targetSubscriptionID.value)) {
    targetSubscriptionID.value = subscriptions.value[0]?.id || 0
  }
  await loadPlans()
  await syncURL()
}

async function selectSubscription(id: number) {
  targetSubscriptionID.value = id
  requestedSKUID.value = 0
  await loadPlans()
  await syncURL()
}

async function selectPlan(plan: PlanCatalogItem, sync = true) {
  selectedPlan.value = plan
  selectedPlanID.value = plan.id
  requestedSKUID.value = Number(route.query.sku) || 0
  await loadSKUs()
  if (sync) await syncURL()
}

async function selectSKU(sku: PlanSKU) {
  selectedSKU.value = sku
  requestedSKUID.value = sku.id
  await syncURL()
}

async function applyPlanFilters() {
  planQuery.value = planDraftQuery.value.trim()
  planOffset.value = 0
  await loadPlans()
  await syncURL()
}

async function clearPlanFilters() {
  planQuery.value = ''
  planDraftQuery.value = ''
  planOffset.value = 0
  await loadPlans()
  await syncURL()
}

async function changePlanPage(value: { offset: number; limit: number }) {
  planOffset.value = value.offset
  planLimit.value = value.limit
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
    message.value = '订单已创建，正在前往订单页面。'
    await router.push('/account/orders')
  } catch (cause: any) {
    actionError.value = cause?.response?.data?.message || '订单创建失败。'
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  await loadSubscriptions()
  if (operation.value !== 'purchase' && !subscriptions.value.length) operation.value = 'purchase'
  await loadPlans()
  await syncURL()
})
</script>
