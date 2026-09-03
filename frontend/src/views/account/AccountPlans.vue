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
        <UiButton v-else-if="selectedPlan || route.query.plan" variant="secondary" type="button" @click="backToCatalog">
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
    <PageAlert v-if="detailError && !selectedPlan" tone="danger" title="套餐详情加载失败">
      {{ detailError }} <UiButton variant="secondary" @click="retryDetail">重试详情</UiButton>
    </PageAlert>
    <div v-if="detailLoading && !selectedPlan" class="commerce-loading-state" role="status">
      <UiIcon name="refresh" />正在加载套餐详情
    </div>

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
              <div v-if="operation === 'renew'"><dt>再次购买效果</dt><dd>{{ renewalEffectLabel(selectedSKU) }}</dd></div>
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
      :sku-total="skuTotal"
      :sku-offset="skuOffset"
      :sku-limit="skuLimit"
      @change-sku-page="changeSKUPage"
      @retry="retryDetail"
      @back="backToCatalog"
      @select-sku="selectDetailSKU"
      @continue="openCheckout"
    />

    <template v-else-if="!detailLoading && !detailError">
      <section v-if="operation === 'purchase'" class="commerce-hub-section" aria-labelledby="active-subscriptions-title">
        <div class="commerce-hub-heading">
          <div>
            <span>当前服务</span>
            <h2 id="active-subscriptions-title">管理现有订阅</h2>
            <p>续费、切换套餐和购买流量包从具体订阅发起。</p>
          </div>
          <small v-if="subscriptionTotal">{{ subscriptionTotal }} 个可管理订阅</small>
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
              <strong>{{ subscription.status === 'expired' ? '额度已用完' : formatDate(subscription.end_at) }}</strong>
            </header>
            <dl>
              <div><dt>剩余流量</dt><dd>{{ formatBytes(Math.max(0, subscription.flow_total - subscription.flow_used)) }}</dd></div>
              <div><dt>设备数</dt><dd>{{ subscription.device_limit > 0 ? subscription.device_limit : '不限' }}</dd></div>
            </dl>
            <div class="commerce-subscription-actions">
              <UiButton variant="secondary" type="button" @click="startOperation('renew', subscription.id)">{{ renewActionLabel(subscription) }}</UiButton>
              <UiButton v-if="subscription.status === 'active'" variant="secondary" type="button" @click="startOperation('change', subscription.id)">切换套餐</UiButton>
              <UiButton v-if="subscription.status === 'active'" variant="secondary" type="button" @click="startOperation('addon', subscription.id)">购买流量包</UiButton>
            </div>
          </article>
        </div>
        <EmptyState
          v-else-if="subscriptionsLoaded && !subscriptionError"
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
            <p>{{ selectedSubscription.sku_name }} · {{ selectedSubscription.status === 'expired' ? '额度已用完，可补充后恢复' : `到期 ${formatDate(selectedSubscription.end_at)}` }}</p>
          </div>
          <dl>
            <div><dt>剩余流量</dt><dd>{{ formatBytes(Math.max(0, selectedSubscription.flow_total - selectedSubscription.flow_used)) }}</dd></div>
            <div><dt>设备数</dt><dd>{{ selectedSubscription.device_limit > 0 ? selectedSubscription.device_limit : '不限' }}</dd></div>
          </dl>
          <UiButton variant="secondary" type="button" @click="changeTarget">更换订阅</UiButton>
        </div>
        <div v-else-if="operationSubscriptions.length" class="commerce-target-grid">
          <button
            v-for="subscription in operationSubscriptions"
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
          v-else-if="subscriptionsLoaded && !subscriptionError"
          icon="plans"
          title="当前没有可操作的订阅"
          description="请先购买套餐。"
        >
          <template #actions><UiButton type="button" @click="returnToOverview">前往新购</UiButton></template>
        </EmptyState>
      </section>

      <TablePager
        v-if="subscriptionTotal > subscriptionLimit && (operation === 'purchase' || !selectedSubscription)"
        :total="subscriptionTotal" :offset="subscriptionOffset" :limit="subscriptionLimit"
        :loading="subscriptionLoading" :page-sizes="[6, 25, 50]" @change="changeSubscriptionPage"
      />

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
            :offer="plan.primary_sku || null"
            :offer-count="plan.active_sku_count"
            :disabled="Boolean(planError) || !plan.primary_sku"
            @select="openPlanDetail(plan.id)"
          />
        </div>
        <EmptyState
          v-else-if="!planLoading && !planError"
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
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createOrder, fetchAccountSubscriptionsPage, fetchPlanCatalogPage, type AdminSubscriptionListItem, type PlanCatalogItem, type PlanSKU, type CatalogOperation } from '../../api/client'
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
import { useCatalogDetail } from '../../composables/useCatalogDetail'
import { useRemoteResource } from '../../composables/useRemoteResource'
import { useRemoteTable } from '../../composables/useRemoteTable'
import { commerceErrorMessage } from '../../utils/commerceErrors'
import { formatBytes, formatCurrency, isPerpetualDate } from '../../utils/format'

type PurchaseOperation = CatalogOperation
const operationOptions = [
  { value: 'purchase', label: '新购', title: '套餐中心', description: '选择套餐，进入详情比较规格并确认订单。' },
  { value: 'renew', label: '续费', title: '续费订阅', description: '选择续费规格并确认服务周期。' },
  { value: 'change', label: '切换套餐', title: '切换套餐', description: '为指定订阅选择新的套餐和规格。' },
  { value: 'addon', label: '流量包', title: '购买流量包', description: '为指定订阅增加可用流量。' },
]
const orderTypeByOperation = { purchase: 'new', renew: 'renewal', change: 'upgrade', addon: 'traffic_pack' }
const route = useRoute(), router = useRouter()
const operation = ref<PurchaseOperation>('purchase')
const targetSubscriptionID = ref(0)
const planOffset = ref(0), planLimit = ref(9)
const subscriptionOffset = ref(0), subscriptionLimit = ref(6)
const query = ref(''), searchDraft = ref('')
const checkoutOpen = ref(false), creating = ref(false)
const actionError = ref(''), checkoutError = ref('')
const eligibleFor = computed(() => operation.value === 'purchase' ? 'manage' : operation.value)

const subscriptionTable = useRemoteTable<AdminSubscriptionListItem>({
  offset: subscriptionOffset, limit: subscriptionLimit,
  fetchPage: ({ signal }) => fetchAccountSubscriptionsPage({
    eligibleFor: eligibleFor.value, offset: subscriptionOffset.value, limit: subscriptionLimit.value,
  }, { signal }),
  errorMessage: '可操作订阅加载失败，请重试。',
})
const { items: subscriptions, total: subscriptionTotal, hasLoaded: subscriptionsLoaded } = subscriptionTable
// The selected ID is resolved independently of the current candidate page.
const targetResource = useRemoteResource<AdminSubscriptionListItem | null>({
  initial: () => null,
  fetch: async ({ signal }) => {
    const id = targetSubscriptionID.value
    const result = await fetchAccountSubscriptionsPage({ subscriptionId: id, eligibleFor: eligibleFor.value, offset: 0, limit: 1 }, { signal })
    const selected = result.items.find(item => item.id === id)
    if (!selected) throw new Error('unavailable subscription')
    return selected
  },
  errorMessage: '目标订阅不存在或不适用于本次操作，请更换订阅。',
})
const selectedSubscription = targetResource.data
const subscriptionLoading = computed(() => subscriptionTable.loading.value || targetResource.loading.value)
const subscriptionError = computed(() => targetResource.error.value || subscriptionTable.error.value)
const operationSubscriptions = subscriptions
const catalog = useRemoteTable<PlanCatalogItem>({
  offset: planOffset, limit: planLimit,
  fetchPage: ({ signal }) => fetchPlanCatalogPage({
    q: query.value || undefined, offset: planOffset.value, limit: planLimit.value, operation: operation.value,
    planId: operation.value === 'renew' || operation.value === 'addon' ? selectedSubscription.value?.plan_id : undefined,
    excludePlanId: operation.value === 'change' ? selectedSubscription.value?.plan_id : undefined,
  }, { signal }),
  errorMessage: '套餐目录加载失败，请重试。',
})
const { items: plans, total: planTotal, loading: planLoading, error: planError } = catalog
const detail = useCatalogDetail()
const { plan: selectedPlan, skus: detailSKUs, selectedSkuId: selectedSKUID, selectedSku: selectedSKU,
  loading: detailLoading, error: detailError, total: skuTotal, offset: skuOffset, limit: skuLimit } = detail
const currentOperation = computed(() => {
  const base = operationOptions.find(item => item.value === operation.value) || operationOptions[0]!
  return operation.value === 'renew' && isPermanentSubscription(selectedSubscription.value)
    ? { value: 'renew', label: '补充额度', title: '补充永久套餐额度', description: '选择同商品规格，为永久订阅补充套餐流量。' } : base
})
const loading = computed(() => subscriptionLoading.value || planLoading.value || detailLoading.value || creating.value)
const catalogVisible = computed(() => operation.value === 'purchase' || Boolean(selectedSubscription.value))
const pageTitle = computed(() => checkoutOpen.value ? '确认订单' : selectedPlan.value?.name || currentOperation.value.title)
const pageDescription = computed(() => checkoutOpen.value ? '核对订单内容后创建订单。' : selectedPlan.value ? '选择规格并继续结算。' : currentOperation.value.description)
const catalogTitle = computed(() => ({
  purchase: '购买新的订阅', renew: `${selectedSubscription.value?.plan_name || ''} ${currentOperation.value.label}`,
  change: '选择新的套餐', addon: `${selectedSubscription.value?.plan_name || ''} 流量包`,
}[operation.value]))
const catalogDescription = computed(() => ({
  purchase: '选择套餐后进入商品详情。',
  renew: isPermanentSubscription(selectedSubscription.value) ? '选择规格并为永久订阅补充套餐流量。' : '选择规格并延长订阅有效期。',
  change: '选择要切换到的套餐。', addon: '选择需要增加的流量。',
}[operation.value]))

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  if (sku.entitlement_mode === 'traffic_addon') return '一次性流量加购'
  if (sku.billing_unit === 'once') return '永久有效 · 流量用完为止'
  const period = `${sku.billing_value} ${unit}`
  return sku.billing_mode === 'one_time' ? `一次性付费 · ${period}有效` : period
}

function renewalEffectLabel(sku: PlanSKU) {
  return ({
    none: '不适用',
    extend_only: '只延长有效期，流量不叠加',
    extend_and_add_quota: '延长有效期，并增加一份套餐流量',
    add_quota_only: '只补充一份套餐流量，永久有效期不变',
  } as Record<string, string>)[sku.renewal_effect] || '按规格配置履约'
}

function isPermanentSubscription(subscription: AdminSubscriptionListItem | null | undefined) {
  return Boolean(subscription && isPerpetualDate(subscription.end_at))
}

function renewActionLabel(subscription: AdminSubscriptionListItem) {
  return isPermanentSubscription(subscription) ? '补充额度' : '续费'
}

function formatDate(value: string) {
  if (!value) return '—'
  if (isPerpetualDate(value)) return '永久有效'
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(value))
}


function catalogQuery() {
  return {
    operation: operation.value,
    ...(targetSubscriptionID.value ? { subscription: String(targetSubscriptionID.value) } : {}),
    ...(query.value ? { q: query.value } : {}),
    page: String(Math.floor(planOffset.value / planLimit.value) + 1), limit: String(planLimit.value),
    subscription_page: String(Math.floor(subscriptionOffset.value / subscriptionLimit.value) + 1),
    subscription_limit: String(subscriptionLimit.value),
  }
}
function detailURL(step = 'detail') {
  if (!selectedPlan.value) return
  return router.replace({ query: { ...catalogQuery(), plan: String(selectedPlan.value.id), sku: String(selectedSKUID.value), step } })
}
function openPlanDetail(id: number) { return router.replace({ query: { ...catalogQuery(), plan: String(id), step: 'detail' } }) }
function backToCatalog() { return router.replace({ query: catalogQuery() }) }
function startOperation(next: Exclude<PurchaseOperation, 'purchase'>, id: number) {
  const target = subscriptions.value.find(item => item.id === id) || (selectedSubscription.value?.id === id ? selectedSubscription.value : null)
  return router.replace({ query: {
    operation: next, subscription: String(id),
    ...((next === 'renew' || next === 'addon') && target ? { plan: String(target.plan_id), step: 'detail' } : {}),
  } })
}
function selectTargetSubscription(id: number) { return startOperation(operation.value as Exclude<PurchaseOperation, 'purchase'>, id) }
function changeTarget() { return router.replace({ query: { operation: operation.value } }) }
function returnToOverview() { return router.replace({ query: { operation: 'purchase' } }) }
function submitSearch() { return router.replace({ query: { ...catalogQuery(), q: searchDraft.value.trim() || undefined, page: '1' } }) }
function clearSearch() { searchDraft.value = ''; return submitSearch() }
function changePlanPage(value: { offset: number; limit: number }) {
  return router.replace({ query: { ...catalogQuery(), page: String(Math.floor(value.offset / value.limit) + 1), limit: String(value.limit) } })
}
function changeSubscriptionPage(value: { offset: number; limit: number }) {
  return router.replace({ query: { ...catalogQuery(), subscription_page: String(Math.floor(value.offset / value.limit) + 1), subscription_limit: String(value.limit) } })
}
function selectDetailSKU(sku: PlanSKU) {
  if (detailLoading.value || detailError.value || !detailSKUs.value.some(item => item.id === sku.id)) return
  selectedSKUID.value = sku.id
  return detailURL()
}
async function changeSKUPage(value: { offset: number; limit: number }) {
  if (await detail.changePage(value)) await detailURL()
}
async function retryDetail() { if (await detail.retry()) await detailURL() }
function openCheckout() { if (selectedSKU.value) return detailURL('checkout') }
function backToDetail() { return detailURL() }

// Route transitions have their own generation: a late target lookup cannot
// start a detail/catalog request for a route that has already been left.
let generation = 0, contextKey = '', subscriptionKey = '', catalogKey = '', detailPlanID = 0
let targetReady: Promise<boolean> = Promise.resolve(true)
onScopeDispose(() => { generation++ })
async function applyRoute(force = false) {
  const current = ++generation
  const nextOperation = String(route.query.operation || 'purchase')
  const nextTarget = Math.max(0, Number(route.query.subscription) || 0)
  const planID = Math.max(0, Number(route.query.plan) || 0)
  const skuID = Math.max(0, Number(route.query.sku) || 0)
  const step = String(route.query.step || '')
  operation.value = operationOptions.some(item => item.value === nextOperation) ? nextOperation as PurchaseOperation : 'purchase'
  targetSubscriptionID.value = operation.value === 'purchase' ? 0 : nextTarget
  planLimit.value = [6, 9, 12].includes(Number(route.query.limit)) ? Number(route.query.limit) : 9
  planOffset.value = (Math.max(1, Number(route.query.page) || 1) - 1) * planLimit.value
  subscriptionLimit.value = [6, 25, 50].includes(Number(route.query.subscription_limit)) ? Number(route.query.subscription_limit) : 6
  subscriptionOffset.value = (Math.max(1, Number(route.query.subscription_page) || 1) - 1) * subscriptionLimit.value
  query.value = String(route.query.q || '').trim()
  searchDraft.value = query.value
  checkoutOpen.value = false
  checkoutError.value = ''
  const nextContext = `${operation.value}:${targetSubscriptionID.value}`
  const contextChanged = contextKey !== nextContext
  if (contextChanged || force) {
    contextKey = nextContext
    detail.close()
    catalog.invalidate()
    plans.value = []
    planTotal.value = 0
    catalogKey = ''
    targetResource.reset()
    targetReady = targetSubscriptionID.value ? targetResource.load() : Promise.resolve(true)
  }
  if (!planID || detailPlanID !== planID) detail.close()
  detailPlanID = planID
  const nextSubscriptions = `${eligibleFor.value}:${subscriptionOffset.value}:${subscriptionLimit.value}`
  if (subscriptionKey !== nextSubscriptions || force) {
    subscriptionKey = nextSubscriptions
    void subscriptionTable.load()
  }
  if (!await targetReady || current !== generation) return
  if (!catalogVisible.value) return
  if (planID) {
    catalog.invalidate()
    catalogKey = ''
    if (!force && selectedPlan.value?.id === planID && (!skuID || detailSKUs.value.some(item => item.id === skuID))) {
      if (skuID) selectedSKUID.value = skuID
    } else if (!await detail.open(planID, operation.value, skuID) || current !== generation) return
    checkoutOpen.value = step === 'checkout' && Boolean(selectedSKU.value)
    return
  }
  const nextCatalog = `${contextKey}:${query.value}:${planOffset.value}:${planLimit.value}`
  if (catalogKey !== nextCatalog || force) {
    catalogKey = nextCatalog
    await catalog.load()
  }
}
function refreshAll() { actionError.value = ''; return applyRoute(true) }
watch(() => route.fullPath, () => { void applyRoute() }, { immediate: true })

async function submitOrder() {
  if (creating.value || !selectedPlan.value || !selectedSKU.value) return
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
</script>
