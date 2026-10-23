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
        <UiButton v-else-if="!selectedPlan && route.query.plan" variant="secondary" type="button" @click="backToCatalog">
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
      <PageAlert v-if="attemptError" tone="danger" title="订单恢复记录无法核对">{{ attemptError }}</PageAlert>
      <p v-if="checkoutAttempt" role="status">上次创建请求已保存。恢复将沿用原订单内容和原确认金额；请恢复上次订单或前往“我的订单”核对。</p>
      <p v-if="savedTargetSubscriptionID">原请求目标订阅：#{{ savedTargetSubscriptionID }}</p>

      <div class="purchase-checkout__layout">
        <div class="purchase-checkout__main">
          <section class="purchase-checkout__section">
            <header>
              <span>{{ checkoutAttempt ? '已提交订单' : currentOperation.label }}</span>
              <h2>订单确认</h2>
            </header>

            <article class="purchase-checkout__product">
              <div>
                <small v-if="!checkoutAttempt">{{ selectedPlan.slug }}</small>
                <h3>{{ checkoutPlanName }}</h3>
                <p>{{ checkoutSKUName }}<template v-if="!checkoutAttempt"> · {{ billingLabel(selectedSKU) }}</template><template v-else-if="savedConfirmation?.billing"> · {{ savedConfirmation.billing }}</template></p>
                <p v-if="operation === 'renew' && quote">按原购买基础价续费，金额以本次报价为准。</p>
                <p v-if="operation === 'change' && quote">原价 {{ formatCurrency(quote.amount_cents, quote.currency) }} · 抵扣 {{ formatCurrency(quote.credit_amount, quote.currency) }}</p>
              </div>
              <strong>{{ checkoutAmountLabel }}</strong>
            </article>

            <dl v-if="!checkoutAttempt && !attemptError" class="purchase-checkout__details">
              <div v-if="selectedSubscription"><dt>目标订阅</dt><dd>{{ selectedSubscription.plan_name }} / #{{ selectedSubscription.id }}</dd></div>
              <div><dt>订单类型</dt><dd>{{ currentOperation.label }}</dd></div>
              <div><dt>规格</dt><dd>{{ selectedSKU.name }}</dd></div>
              <div><dt>计费规格</dt><dd>{{ billingLabel(selectedSKU) }}</dd></div>
              <template v-if="operation === 'addon'">
                <div><dt>附加流量</dt><dd>{{ formatBytes(selectedSKU.grant_traffic_bytes) }}</dd></div>
                <div><dt>到期时间</dt><dd>保持目标订阅不变</dd></div>
              </template>
              <template v-if="operation === 'reset'">
                <div><dt>恢复可用流量</dt><dd>{{ formatBytes(quote?.traffic_bytes || 0) }}</dd></div>
                <div><dt>到期时间</dt><dd>{{ formatDate(selectedSubscription!.end_at) }}（保持不变）</dd></div>
              </template>
              <div v-else-if="operation === 'renew'"><dt>再次购买效果</dt><dd>{{ renewalEffectLabel(selectedSKU) }}</dd></div>
              <template v-else-if="operation !== 'addon'">
                <div v-if="operation === 'change' && quote"><dt>到期时间</dt><dd>{{ formatDate(quote.end_at!) }}{{ quote.change_mode === 'full_term' ? '（按履约时间起算，此处为预计日期）' : '（保持不变）' }}</dd></div>
                <div v-if="operation === 'change' && quote"><dt>本周期已用</dt><dd>{{ formatBytes(quote.used_bytes) }}</dd></div>
                <div><dt>套餐流量</dt><dd>{{ formatBytes(selectedPlan.traffic_bytes) }}</dd></div>
                <div><dt>设备数</dt><dd>{{ selectedPlan.device_limit > 0 ? `${selectedPlan.device_limit} 台` : '不限设备' }}</dd></div>
                <div><dt>速度</dt><dd>{{ selectedPlan.speed_limit_mbps > 0 ? `${selectedPlan.speed_limit_mbps} Mbps` : '不限速' }}</dd></div>
              </template>
            </dl>
          </section>
          <section v-if="selectableDiscount && !attemptError && (!checkoutAttempt || discountConfirmation)" class="purchase-checkout__section" :aria-label="operation === 'purchase' ? '新购优惠' : '本单优惠'">
            <h3>优惠</h3>
            <template v-if="checkoutAttempt && discountConfirmation">
              <p>已确认优惠：{{ discountConfirmation.name }}。{{ discountConfirmation.message }}</p>
              <p>{{ discountAgreementLabel }}</p>
              <p v-if="discountConfirmation.expiresAt">原确认优惠有效至 <TimeBadge :value="discountConfirmation.expiresAt" />。</p>
            </template>
            <template v-else>
              <p v-if="discountProvidersLoading" role="status">正在读取可用优惠…</p>
              <PageAlert v-if="discountProvidersError" tone="danger" title="优惠列表读取失败">{{ discountProvidersError }} 不使用优惠仍可继续购买。<UiButton variant="secondary" :disabled="creating" @click="providerResource.load()">重试优惠列表</UiButton></PageAlert>
              <FormField label="优惠方案" name="purchase-discount-provider" v-slot="{ controlAttrs }">
                <UiSelect v-model="discountProviderId" v-bind="controlAttrs" :disabled="creating || discountProvidersLoading" :options="discountProviderOptions" />
              </FormField>
              <p v-if="selectedDiscountProvider">{{ selectedDiscountProvider.description }}</p>
              <template v-if="discountProviderId">
                <FormField label="优惠码（可选）" name="purchase-discount-code" v-slot="{ controlAttrs }">
                  <UiInput v-model="discountCode" v-bind="controlAttrs" maxlength="256" autocomplete="off" :disabled="creating" />
                </FormField>
                <UiButton type="button" variant="secondary" :disabled="creating || discountLoading || !selectedDiscountProvider || Boolean(discountProvidersError)" @click="loadDiscountQuote">核对优惠</UiButton>
                <p v-if="discountLoading" role="status">正在核对本次优惠…</p>
                <PageAlert v-if="discountError" tone="danger" title="优惠报价失败">{{ discountError }}</PageAlert>
                <p v-if="discountExpired" role="status">优惠报价已过期，请重新核对优惠后确认。</p>
                <p v-if="discountQuote">{{ discountQuote.discount?.offer.message }}<br>优惠有效至 <TimeBadge :value="discountQuote.discount!.offer.expires_at" />。{{ discountAgreementLabel }}</p>
              </template>
            </template>
          </section>
          <section v-if="operation === 'renew' && discountConfirmation && !attemptError" class="purchase-checkout__section" aria-label="继承优惠">
            <h3>继承的长期优惠约定</h3>
            <p>本次续费沿用原订阅的优惠约定，不能另选优惠提供方。{{ discountConfirmation.message }}</p>
            <p>{{ discountAgreementLabel }}</p>
            <p v-if="discountConfirmation.expiresAt">{{ checkoutAttempt ? '原确认优惠有效至' : '本次优惠有效至' }} <TimeBadge :value="discountConfirmation.expiresAt" />。</p>
          </section>
        </div>

        <aside class="purchase-checkout__summary">
          <span>{{ quote?.change_mode === 'full_term' ? '新套餐完整价格' : '应付金额' }}</span>
          <strong>{{ checkoutAmountLabel }}</strong>
          <dl>
            <template v-if="discountConfirmation">
              <div><dt>基础价格</dt><dd>{{ formatCurrency(discountConfirmation.basePrice, discountConfirmation.currency) }}</dd></div>
              <div><dt>本次优惠</dt><dd>−{{ formatCurrency(discountConfirmation.discountAmount, discountConfirmation.currency) }}</dd></div>
            </template>
            <div v-if="operation === 'change' && quote"><dt>规格原价</dt><dd>{{ formatCurrency(quote.amount_cents, quote.currency) }}</dd></div>
            <div v-if="operation === 'change' && quote"><dt>{{ quote.change_mode === 'full_term' ? '旧权益用于本单' : '原套餐抵扣' }}</dt><dd>−{{ formatCurrency(quote.credit_amount, quote.currency) }}</dd></div>
            <template v-if="operation === 'change' && quote?.change_mode === 'full_term'">
              <div><dt>旧权益结算总额</dt><dd>{{ formatWalletMoney(quote.settlement_amount ?? '0', quote.currency) }}</dd></div>
              <div><dt>结余入余额</dt><dd>{{ formatWalletMoney(quote.surplus_amount ?? '0', quote.currency) }}</dd></div>
              <div><dt>另需支付</dt><dd>{{ formatWalletMoney(quote.additional_amount ?? '0', quote.currency) }}</dd></div>
            </template>
            <div><dt>商品</dt><dd>{{ checkoutPlanName }}</dd></div>
            <div><dt>规格</dt><dd>{{ checkoutSKUName }}</dd></div>
          </dl>
          <PageAlert v-if="checkoutError" tone="danger" title="无法创建订单">{{ checkoutError }}</PageAlert>
          <PageAlert v-if="!checkoutAttempt && !attemptError && quoteError" tone="danger" title="报价失败">{{ quoteError }}<UiButton variant="secondary" @click="loadQuote">重新报价</UiButton></PageAlert>
          <p v-if="!checkoutAttempt && renewalDiscountExpired" role="status">继承的优惠报价已过期，请重新报价后确认。</p>
          <p v-if="!checkoutAttempt && !attemptError && !quoteError && needsQuote && !quote" role="status">请核对本次报价后确认订单。<UiButton variant="secondary" :disabled="quoteLoading" @click="loadQuote">重新报价</UiButton></p>
          <CheckoutPayment v-show="!checkoutAttempt" name="account.checkout.payment" :key="checkoutIntent" embedded handoff-to-funding :actor-id="app.user.id" :currency="checkoutCurrency" :create-order="ensureOrder" :create-only="submitOrder" :disabled="creating || quoteLoading || (needsQuote && !quote) || Boolean(attemptError) || (discountRequired && (!discountQuote || discountLoading || Boolean(discountError) || Boolean(discountProvidersError)))" @paid="router.push('/account/orders')" @created="id => router.push(`/account/orders/${id}`)" />
          <UiButton v-if="checkoutAttempt" :disabled="creating || Boolean(attemptError)" :loading="creating" @click="restoreOrder">恢复上次订单</UiButton>
          <p>订单及付款进度可在“我的订单”中查看。</p>
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
      :quote="quote"
      :renewal-recovering="operation === 'renew' && !!selectedSubscription?.renewal_until"
      :quote-loading="quoteLoading"
      :quote-error="quoteError"
      @retry-quote="loadQuote"
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
      <section v-if="operation === 'purchase' && (!subscriptionsLoaded || subscriptionLoading || subscriptionError || subscriptionTotal > 0)" class="commerce-hub-section" aria-labelledby="active-subscriptions-title">
        <div class="commerce-hub-heading">
          <div>
            <span>当前服务</span>
            <h2 id="active-subscriptions-title">管理现有订阅</h2>
            <p>续费、切换套餐、购买流量包和重置流量从具体订阅发起。</p>
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
                <p v-if="subscription.renewal_until">服务已停止 · 续费保留至 <TimeBadge :value="subscription.renewal_until" /></p>
              </div>
              <strong>{{ formatDate(subscription.end_at) }}</strong>
            </header>
            <dl>
              <div><dt>{{ subscription.renewal_until ? '结束时剩余流量' : '剩余流量' }}</dt><dd>{{ formatBytes(Math.max(0, subscription.flow_total - subscription.flow_used)) }}<span v-if="subscription.flow_used >= subscription.flow_total"> · 本周期流量已用完</span></dd></div>
              <div><dt>设备数</dt><dd>{{ subscription.device_limit > 0 ? subscription.device_limit : '不限' }}</dd></div>
            </dl>
            <div class="commerce-subscription-actions">
              <UiButton v-if="subscription.can_renew" variant="secondary" type="button" @click="startOperation('renew', subscription.id)">{{ renewActionLabel(subscription) }}</UiButton>
              <UiButton v-if="subscription.can_change" variant="secondary" type="button" @click="startOperation('change', subscription.id)">切换套餐</UiButton>
              <UiButton v-if="subscription.can_addon" variant="secondary" type="button" @click="startOperation('addon', subscription.id)">购买流量包</UiButton>
              <UiButton v-if="subscription.can_reset" variant="secondary" type="button" @click="startOperation('reset', subscription.id)">购买流量重置</UiButton>
            </div>
          </article>
        </div>
        <EmptyState
          v-else-if="subscriptionsLoaded && !subscriptionError"
          icon="plans"
          title="当前没有可操作订阅"
          description="从下方选择套餐即可创建新的订阅。"
        />
      </section>

      <section v-else-if="operation !== 'purchase'" class="commerce-hub-section" aria-labelledby="target-subscription-title">
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
            <p>{{ selectedSubscription.sku_name }} · {{ selectedSubscription.status === 'expired' ? '服务已停止，可在保留期内续费恢复' : `到期 ${formatDate(selectedSubscription.end_at)}` }}</p>
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
              <small :title="subscription.sku_name">{{ subscription.sku_name }}</small>
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
import CheckoutPayment from '../../plugins/CheckoutPayment.vue'
import { ensureCheckoutOrder, isDefiniteOrderRejection, readCheckoutOrderDraft, restoreCheckoutIntent, newCheckoutIntent, type OrderDraft } from '../../composables/checkoutOrder'
import { useAppStore } from '../../stores/app'
import TimeBadge from '../../components/TimeBadge.vue'
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createOrder, previewOrder, type OrderPreview, fetchAccountSubscriptionsPage, fetchPlanCatalogPage, type AdminSubscriptionListItem, type PlanCatalogItem, type PlanSKU, type CatalogOperation } from '../../api/client'
import CommercePlanCard from '../../components/CommercePlanCard.vue'
import CommercePlanDetail from '../../components/CommercePlanDetail.vue'
import EmptyState from '../../components/EmptyState.vue'
import PageAlert from '../../components/PageAlert.vue'
import PageHeader from '../../components/PageHeader.vue'
import TablePager from '../../components/TablePager.vue'
import UiButton from '../../components/UiButton.vue'
import UiInput from '../../components/UiInput.vue'
import UiSelect from '../../components/UiSelect.vue'
import FormField from '../../components/FormField.vue'
import { fetchDiscountProviders, previewDiscountOrder, previewOperationDiscountOrder, validateDiscountOrderQuote, type DiscountProvider, type OrderDiscountSelection } from '../../api/commerceDiscounts'
import UiIcon from '../../components/UiIcon.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../../components/WorkbenchFilterInput.vue'
import { useCatalogDetail } from '../../composables/useCatalogDetail'
import { useRemoteResource } from '../../composables/useRemoteResource'
import { useRemoteTable } from '../../composables/useRemoteTable'
import { commerceErrorMessage } from '../../utils/commerceErrors'
import { formatWalletMoney } from '../../utils/walletMoney'
import { formatBytes, formatCurrency, isPerpetualDate } from '../../utils/format'

type PurchaseOperation = CatalogOperation
const operationOptions = [
  { value: 'purchase', label: '新购', title: '套餐中心', description: '选择套餐，进入详情比较规格并确认订单。' },
  { value: 'renew', label: '续费', title: '续费订阅', description: '选择续费规格并确认服务周期。' },
  { value: 'change', label: '切换套餐', title: '切换套餐', description: '为指定订阅选择新的套餐和规格。' },
  { value: 'addon', label: '流量包', title: '购买流量包', description: '为指定订阅当前周期增加可用流量；定期重置时失效。' },
  { value: 'reset', label: '重置流量', title: '重置本周期流量', description: '恢复本周期套餐额度，保持原到期和下次自动重置日期。' },
]
const orderTypeByOperation = { purchase: 'new', renew: 'renewal', change: 'upgrade', addon: 'traffic_pack', reset: 'traffic_reset' }
const route = useRoute(), router = useRouter()
const app = useAppStore()
const checkoutIntent = computed(() => String(route.query.checkout_intent || restoreCheckoutIntent(localStorage, `zboard-checkout-active:${app.user.id}:${operation.value}:${selectedSKU.value?.id || 0}:${targetSubscriptionID.value}`)))
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
    planId: operation.value === 'renew' || operation.value === 'addon' || operation.value === 'reset' ? selectedSubscription.value?.plan_id : undefined,
    excludePlanId: operation.value === 'change' ? selectedSubscription.value?.plan_id : undefined,
  }, { signal }),
  errorMessage: '套餐目录加载失败，请重试。',
})
const { items: plans, total: planTotal, loading: planLoading, error: planError } = catalog
const detail = useCatalogDetail()
const { plan: selectedPlan, skus: detailSKUs, selectedSkuId: selectedSKUID, selectedSku: selectedSKU,
  loading: detailLoading, error: detailError, total: skuTotal, offset: skuOffset, limit: skuLimit } = detail
interface DiscountConfirmation { name: string; basePrice: number; discountAmount: number; payable: number; currency: string; message: string; scope?: 'initial_only' | 'recurring' | 'order_only'; expiresAt?: string }
interface CheckoutConfirmation {
  version: 1; skuId: number; orderType: string; quoteFingerprint?: string
  payable: number; currency: string; planName: string; skuName: string; billing: string
  discount?: DiscountConfirmation
}
interface CheckoutRequest { skuId: number; orderType: string; targetSubscriptionId?: number; quoteFingerprint?: string; discount?: OrderDiscountSelection; confirmation?: CheckoutConfirmation }
const checkoutScope = computed(() => `zboard-checkout:${app.user.id}:${checkoutIntent.value}`)
const checkoutAttempt = ref<OrderDraft<CheckoutRequest> | null>(null)
const savedTargetSubscriptionID = computed(() => {
  const id = checkoutAttempt.value?.request.targetSubscriptionId
  return Number.isSafeInteger(id) && id! > 0 ? id : undefined
})
const attemptError = ref('')
function refreshCheckoutAttempt() {
  try { checkoutAttempt.value = readCheckoutOrderDraft<CheckoutRequest>(localStorage, checkoutScope.value); attemptError.value = '' }
  catch (cause: any) { checkoutAttempt.value = null; attemptError.value = cause.message }
}
watch(() => [app.user.id, checkoutScope.value, checkoutOpen.value], refreshCheckoutAttempt, { flush: 'sync', immediate: true })
const discountProviderId = ref(''), discountCode = ref(''), discountExpired = ref(false)
const providerResource = useRemoteResource<DiscountProvider[]>({ initial: () => [], fetch: ({ signal }) => fetchDiscountProviders(signal), errorMessage: '优惠列表暂时无法读取，请重试。' })
const { data: discountProviders, loading: discountProvidersLoading, error: discountProvidersError } = providerResource
const selectableDiscount = computed(() => ['purchase', 'change', 'reset', 'addon'].includes(operation.value))
const operationProviders = computed(() => discountProviders.value.filter(provider => operation.value === 'purchase' || provider.operations?.includes(orderTypeByOperation[operation.value] as 'upgrade' | 'traffic_reset' | 'traffic_pack')))
const discountProviderOptions = computed(() => [{ value: '', label: '不使用优惠' }, ...operationProviders.value.map(provider => ({ value: provider.provider_id, label: provider.name, disabled: Boolean(discountProvidersError.value) }))])
const selectedDiscountProvider = computed(() => operationProviders.value.find(provider => provider.provider_id === discountProviderId.value))
const discountRequired = computed(() => selectableDiscount.value && Boolean(discountProviderId.value))
const discountResource = useRemoteResource<OrderPreview | null>({
  initial: () => null,
  fetch: ({ signal }) => operation.value === 'purchase'
    ? previewDiscountOrder(selectedSKUID.value, selectedSKU.value!.currency, { provider_id: discountProviderId.value, selection: discountCode.value }, signal)
    : previewOperationDiscountOrder(selectedSKUID.value, targetSubscriptionID.value, orderTypeByOperation[operation.value] as 'upgrade' | 'traffic_reset' | 'traffic_pack', selectedSKU.value!.currency, { provider_id: discountProviderId.value, selection: discountCode.value }, signal),
  errorMessage: (cause: any) => commerceErrorMessage(cause, cause?.message || '优惠报价失败，请重新核对。'),
})
const { data: discountQuote, loading: discountLoading, error: discountError } = discountResource
let discountExpiryTimer: ReturnType<typeof setTimeout> | undefined
watch(discountQuote, value => {
  clearTimeout(discountExpiryTimer)
  if (value?.discount) discountExpiryTimer = setTimeout(() => { discountResource.reset(); discountExpired.value = true }, Math.min(2_147_483_647, Math.max(0, Date.parse(value.discount.offer.expires_at) - Date.now())))
})
onScopeDispose(() => clearTimeout(discountExpiryTimer))
watch(() => [app.user.id, operation.value, selectedSKUID.value, targetSubscriptionID.value, selectedPlan.value?.id], () => {
  discountProviderId.value = ''; discountCode.value = ''; discountResource.reset(); discountExpired.value = false
}, { flush: 'sync' })
watch(() => [discountProviderId.value, discountCode.value], () => { discountResource.reset(); discountExpired.value = false }, { flush: 'sync' })
watch(() => [app.user.id, checkoutOpen.value, operation.value], () => {
  providerResource.reset()
  if (checkoutOpen.value && selectableDiscount.value && !checkoutAttempt.value) void providerResource.load()
})
async function loadDiscountQuote() {
  if (checkoutAttempt.value || creating.value || !discountRequired.value || !selectedDiscountProvider.value || discountProvidersError.value) return
  discountResource.reset(); discountExpired.value = false
  await discountResource.load()
}
const savedConfirmation = computed(() => {
  const request = checkoutAttempt.value?.request, value = request?.confirmation
  return value && value.version === 1 && value.skuId === request?.skuId && value.orderType === request?.orderType
    && value.quoteFingerprint === request?.quoteFingerprint && Number.isSafeInteger(value.payable) && value.payable >= 0
    && typeof value.currency === 'string' && /^[A-Z]{3}$/.test(value.currency)
    ? value : undefined
})
const needsQuote = computed(() => operation.value === 'renew' || operation.value === 'change' || operation.value === 'reset')
const quoteResource = useRemoteResource<OrderPreview | null>({
  initial: () => null,
  fetch: async ({ signal }) => {
    const value = await previewOrder(selectedSKUID.value, targetSubscriptionID.value, { signal })
    if (value.discount) {
      if (operation.value !== 'renew') throw new Error('本次操作不支持优惠报价。')
      return validateDiscountOrderQuote(value, { orderType: 'renewal' })
    }
    return value
  },
  errorMessage: (cause: any) => commerceErrorMessage(cause, cause?.message || '报价失败，请重新确认套餐和规格。'),
})
const { data: currentQuote, loading: quoteLoading, error: baselineQuoteError } = quoteResource
const quoteError = computed(() => discountRequired.value ? '' : baselineQuoteError.value)
const quote = computed(() => checkoutAttempt.value || attemptError.value ? null : discountRequired.value && operation.value !== 'purchase' ? discountError.value ? null : discountQuote.value : quoteError.value ? null : currentQuote.value)
const renewalDiscountExpired = ref(false)
let renewalExpiryTimer: ReturnType<typeof setTimeout> | undefined
watch(currentQuote, value => {
  clearTimeout(renewalExpiryTimer)
  if (value?.discount) renewalExpiryTimer = setTimeout(() => { quoteResource.reset(); renewalDiscountExpired.value = true }, Math.min(2_147_483_647, Math.max(0, Date.parse(value.discount.offer.expires_at) - Date.now())))
})
onScopeDispose(() => clearTimeout(renewalExpiryTimer))
async function loadQuote() {
  if (discountRequired.value && operation.value !== 'purchase') return loadDiscountQuote()
  if (checkoutAttempt.value || attemptError.value || creating.value) return
  quoteResource.reset(); renewalDiscountExpired.value = false
  await quoteResource.load()
}
watch(() => Boolean(checkoutAttempt.value || attemptError.value), locked => { if (locked) quoteResource.reset() }, { flush: 'sync' })
watch(() => [app.user.id, operation.value, selectedSKUID.value, targetSubscriptionID.value, selectedPlan.value?.id], () => {
  quoteResource.reset(); renewalDiscountExpired.value = false
  if (!checkoutAttempt.value && !attemptError.value && needsQuote.value && selectedSKU.value && selectedSubscription.value) void loadQuote()
})
const discountConfirmation = computed<DiscountConfirmation | undefined>(() => {
  if (checkoutAttempt.value) return savedConfirmation.value?.discount
  const value = discountRequired.value ? discountQuote.value : operation.value === 'renew' ? quote.value : null
  if (!value?.discount) return undefined
  return { name: operation.value !== 'renew' ? selectedDiscountProvider.value?.name || '本次优惠' : '原长期优惠约定',
    basePrice: value.base_price_minor!, discountAmount: value.discount_amount!, payable: value.payable_amount,
    currency: value.currency, message: value.discount.offer.message || '', scope: value.discount.offer.agreement!.scope,
    expiresAt: value.discount.offer.expires_at }
})
const discountAgreementLabel = computed(() => {
  const value = discountConfirmation.value
  if (!value) return ''
  const base = formatCurrency(value.basePrice, value.currency)
  return value.scope === 'recurring'
    ? `长期优惠约定：续费基础价已锁定为 ${base}；后续续费沿用该约定，优惠金额以每次有效报价为准，不保证固定。`
    : value.scope === 'initial_only' ? `本次优惠仅限首单，续费基础价为 ${base}。`
    : value.scope === 'order_only' ? operation.value === 'change' ? `本次优惠仅用于本单，换购后的续费基础价为 ${base}。` : '本次优惠仅用于本单，不改变原订阅的续费基础价。'
    : '原优惠约定范围请核对原订单。'
})
const checkoutPayable = computed(() => checkoutAttempt.value ? savedConfirmation.value?.payable : discountConfirmation.value?.payable ?? (discountRequired.value ? undefined : needsQuote.value ? quote.value?.payable_amount : selectedSKU.value?.price_cents))
const checkoutCurrency = computed(() => savedConfirmation.value?.currency || quote.value?.currency || selectedSKU.value?.currency || 'CNY')
const checkoutAmountLabel = computed(() => checkoutAttempt.value || attemptError.value
  ? savedConfirmation.value ? formatCurrency(savedConfirmation.value.payable, savedConfirmation.value.currency) : '金额未知，请核对原订单'
  : checkoutPayable.value == null ? '正在核算…' : formatCurrency(checkoutPayable.value, checkoutCurrency.value))
const checkoutPlanName = computed(() => checkoutAttempt.value ? savedConfirmation.value?.planName || '原商品待核对' : selectedPlan.value?.name)
const checkoutSKUName = computed(() => checkoutAttempt.value ? savedConfirmation.value?.skuName || '原规格待核对' : selectedSKU.value?.name)
const currentOperation = computed(() => {
  const base = operationOptions.find(item => item.value === operation.value) || operationOptions[0]!
  if (operation.value === 'renew' && selectedSubscription.value?.renewal_until) return { ...base, title: '续费恢复订阅', description: '服务已停止，付款完成后恢复服务并开始新的套餐周期和额度。' }
  return operation.value === 'renew' && isPermanentSubscription(selectedSubscription.value)
    ? { value: 'renew', label: '补充额度', title: '补充永久套餐额度', description: '选择同商品规格，为永久订阅补充套餐流量。' } : base
})
const loading = computed(() => subscriptionLoading.value || planLoading.value || detailLoading.value || creating.value)
const catalogVisible = computed(() => operation.value === 'purchase' || Boolean(selectedSubscription.value))
const pageTitle = computed(() => checkoutOpen.value ? '确认订单' : selectedPlan.value?.name || currentOperation.value.title)
const pageDescription = computed(() => checkoutOpen.value ? '核对订单内容并选择支付方式。' : selectedPlan.value ? '选择规格并继续结算。' : currentOperation.value.description)
const catalogTitle = computed(() => ({
  purchase: '购买新的订阅', renew: `${selectedSubscription.value?.plan_name || ''} ${currentOperation.value.label}`,
  change: '选择新的套餐', addon: `${selectedSubscription.value?.plan_name || ''} 流量包`, reset: '购买流量重置',
}[operation.value]))
const catalogDescription = computed(() => ({
  purchase: '选择套餐后进入商品详情。',
  renew: isPermanentSubscription(selectedSubscription.value) ? '选择规格并为永久订阅补充套餐流量。' : '选择规格并延长订阅有效期。',
  change: '选择要切换到的套餐。', addon: '选择需要增加的流量。', reset: '恢复目标订阅的一整份套餐流量，剩余额度不会叠加。',
}[operation.value]))

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  if (sku.entitlement_mode === 'traffic_reset') return '一次性重置流量'
  if (sku.entitlement_mode === 'traffic_addon') return '一次性流量加购'
  if (sku.billing_unit === 'once') return '永久有效 · 流量用完为止'
  const period = `${sku.billing_value} ${unit}`
  return sku.billing_mode === 'one_time' ? `一次性付费 · ${period}有效` : period
}

function renewalEffectLabel(sku: PlanSKU) {
  if (operation.value === 'renew' && selectedSubscription.value?.renewal_until) return '恢复服务，并开始新的套餐周期和流量额度'
  if (sku.billing_unit !== 'once') return '只延长有效期，保留当前用量和流量重置日期'
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
  if (subscription.renewal_until) return '续费恢复'
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
    ...((next === 'renew' || next === 'addon' || next === 'reset') && target ? { plan: String(target.plan_id), step: 'detail' } : {}),
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
async function openCheckout() { if (selectedSKU.value && (!needsQuote.value || quote.value)) { await detailURL('checkout'); await router.replace({ query: { ...route.query, checkout_intent: newCheckoutIntent() } }) } }
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

async function ensureOrder(): Promise<number> {
  const requestScope = checkoutScope.value
  const sameScope = () => checkoutScope.value === requestScope
  refreshCheckoutAttempt()
  if (attemptError.value) throw new Error(attemptError.value)
  if (!checkoutAttempt.value && (!app.user.id || !selectedSKU.value || (needsQuote.value && !quote.value) || (operation.value !== 'purchase' && !selectedSubscription.value))) throw new Error('请确认规格、目标订阅和报价。')
  if (!checkoutAttempt.value && discountRequired.value && (!discountQuote.value || discountError.value || discountLoading.value || discountProvidersError.value || Date.parse(discountQuote.value.discount!.offer.expires_at) <= Date.now())) {
    discountResource.reset()
    throw new Error('请重新核对有效优惠报价后确认订单。')
  }
  if (!checkoutAttempt.value && quote.value?.discount && Date.parse(quote.value.discount.offer.expires_at) <= Date.now()) {
    quoteResource.reset(); renewalDiscountExpired.value = true
    throw new Error('继承的优惠报价已过期，请重新报价后确认。')
  }
  const request: CheckoutRequest = checkoutAttempt.value?.request || { skuId: selectedSKU.value!.id, orderType: orderTypeByOperation[operation.value], targetSubscriptionId: operation.value === 'purchase' ? undefined : selectedSubscription.value?.id, quoteFingerprint: discountRequired.value ? discountQuote.value!.quote_fingerprint : needsQuote.value ? quote.value?.quote_fingerprint : undefined,
    ...(discountRequired.value ? { discount: { provider_id: discountProviderId.value, selection: discountCode.value } } : {}) }
  if (!checkoutAttempt.value) {
    const payable = checkoutPayable.value
    if (!Number.isSafeInteger(payable) || payable! < 0) throw new Error('本次订单金额尚未核对，请重新确认报价。')
    request.confirmation = { version: 1, skuId: request.skuId, orderType: request.orderType, quoteFingerprint: request.quoteFingerprint,
      payable: payable!, currency: checkoutCurrency.value, planName: selectedPlan.value!.name, skuName: selectedSKU.value!.name, billing: billingLabel(selectedSKU.value!),
      ...(discountConfirmation.value ? { discount: discountConfirmation.value } : {}) }
  }
  creating.value = true; checkoutError.value = ''
  try {
    const result = ensureCheckoutOrder(localStorage, requestScope, request, (saved, key) => { const { skuId, confirmation: _confirmation, ...options } = saved; return createOrder(skuId, { ...options, idempotencyKey: key }) })
    refreshCheckoutAttempt()
    return await result
  } catch (cause) {
    const message = commerceErrorMessage(cause, '订单创建结果暂无法确认，请恢复上次订单或到“我的订单”核对。')
    if (isDefiniteOrderRejection(cause)) {
      localStorage.removeItem(requestScope)
      if (sameScope()) {
        discountResource.reset()
        if (needsQuote.value) quoteResource.reset()
        await router.replace({ query: { ...route.query, checkout_intent: newCheckoutIntent() } })
        checkoutError.value = `${message} 请重新核对报价后确认创建。`
      }
    } else if (sameScope()) checkoutError.value = message
    throw cause
  }
  finally { refreshCheckoutAttempt(); creating.value = false }
}
async function restoreOrder() {
  if (creating.value) return
  const scope = checkoutScope.value
  try { const id = await ensureOrder(); if (checkoutScope.value === scope) await router.push(`/account/orders/${id}`) }
  catch { /* The original request and its error stay visible for explicit recovery. */ }
}
async function submitOrder() {
  if (creating.value || !selectedPlan.value || !selectedSKU.value || (needsQuote.value && !quote.value)) return
  if (operation.value !== 'purchase' && !selectedSubscription.value) {
    checkoutError.value = '请选择目标订阅后再创建订单。'
    return
  }
  creating.value = true
  checkoutError.value = ''
  try {
    await ensureOrder()
    await router.push('/account/orders')
  } catch {
    // ensureOrder owns the visible failure and preserves the submitted intent.
  } finally {
    creating.value = false
  }
}
</script>
