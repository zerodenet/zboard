<template>
  <section class="storefront-detail">
    <button class="storefront-back" type="button" @click="$emit('back')">
      <UiIcon name="chevron" />返回套餐列表
    </button>

    <div class="storefront-detail__layout">
      <div class="storefront-detail__main">
        <header class="storefront-detail__hero">
          <span>{{ plan.slug }}</span>
          <h1>{{ plan.name }}</h1>
          <p>{{ plan.summary || '稳定、透明的订阅服务。' }}</p>
        </header>

        <div class="storefront-entitlement-grid">
          <template v-if="mode === 'addon'">
            <article>
              <span>附加流量</span>
              <strong>{{ selectedSku ? formatBytes(selectedSku.grant_traffic_bytes) : '按规格' }}</strong>
            </article>
            <article>
              <span>速度</span>
              <strong>保持当前</strong>
            </article>
            <article>
              <span>到期时间</span>
              <strong>保持当前</strong>
            </article>
          </template>
          <template v-else>
            <article>
              <span>套餐流量</span>
              <strong>{{ formatBytes(plan.traffic_bytes) }}</strong>
            </article>
            <article>
              <span>速度</span>
              <strong>{{ plan.speed_limit_mbps > 0 ? `${plan.speed_limit_mbps} Mbps` : '不限速' }}</strong>
            </article>
            <article>
              <span>设备数</span>
              <strong>{{ plan.device_limit > 0 ? `${plan.device_limit} 台` : '不限设备' }}</strong>
            </article>
          </template>
        </div>

        <section class="storefront-specifications">
          <div class="storefront-section-heading">
            <div>
              <span>{{ operationLabel }}</span>
              <h2>选择规格</h2>
            </div>
            <small v-if="skus.length">{{ skus.length }} 个可用规格</small>
          </div>

          <div v-if="loading" class="commerce-loading-state">
            <UiIcon name="refresh" />正在加载规格
          </div>
          <PageAlert v-else-if="error" tone="danger" title="规格加载失败">{{ error }}</PageAlert>
          <div v-else-if="skus.length" class="storefront-sku-grid" role="radiogroup" :aria-label="`${plan.name} 可用规格`">
            <button
              v-for="sku in skus"
              :key="sku.id"
              type="button"
              role="radio"
              :aria-checked="sku.id === selectedSkuId"
              :class="{ active: sku.id === selectedSkuId }"
              @click="$emit('select-sku', sku)"
            >
              <span class="storefront-sku-grid__check"><UiIcon name="check" /></span>
              <div>
                <strong>{{ sku.name }}</strong>
                <small>{{ billingLabel(sku) }}</small>
              </div>
              <b>{{ formatCurrency(sku.price_cents, sku.currency) }}</b>
              <p v-if="mode === 'addon' && sku.grant_traffic_bytes > 0">
                增加 {{ formatBytes(sku.grant_traffic_bytes) }} 流量
              </p>
            </button>
          </div>
          <div v-else class="storefront-empty-specifications">
            <strong>当前没有可用规格</strong>
            <span>请稍后再试。</span>
          </div>
        </section>
      </div>

      <aside class="storefront-order-summary">
        <span>订单摘要</span>
        <h2>{{ plan.name }}</h2>
        <dl>
          <div v-if="targetName"><dt>目标订阅</dt><dd>{{ targetName }}</dd></div>
          <div><dt>操作</dt><dd>{{ operationLabel }}</dd></div>
          <div><dt>规格</dt><dd>{{ selectedSku?.name || '请选择' }}</dd></div>
          <div><dt>服务周期</dt><dd>{{ selectedSku ? billingLabel(selectedSku) : '—' }}</dd></div>
        </dl>
        <div class="storefront-order-summary__total">
          <span>应付金额</span>
          <strong>{{ selectedSku ? formatCurrency(selectedSku.price_cents, selectedSku.currency) : '—' }}</strong>
        </div>
        <div v-if="hasPurchasePolicies" class="storefront-policy-links">
          <strong><UiIcon name="info" />购买前请确认服务规则</strong>
          <p>以下文档可能影响服务使用、退款资格及公平使用限制，请在继续结算前阅读。</p>
          <div>
            <button v-for="document in purchaseDocuments" :key="document.slug" type="button" @click="openPolicy(document.title, document.content)">
              <span>{{ document.title }}</span><UiIcon name="chevron" />
            </button>
          </div>
          <label class="storefront-policy-consent">
            <UiCheckbox v-model="purchasePoliciesAccepted" />
            <span>我已阅读并同意以上与本次购买相关的服务规则</span>
          </label>
        </div>
        <UiButton type="button" :disabled="!selectedSku || (hasPurchasePolicies && !purchasePoliciesAccepted)" @click="$emit('continue')">
          继续结算<UiIcon name="chevron" />
        </UiButton>
      </aside>
    </div>

    <LegalContentDialog
      :open="Boolean(activePolicyContent)"
      :title="activePolicyTitle"
      :content="activePolicyContent"
      :profile="profile"
      @close="closePolicy"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { PlanCatalogItem, PlanSKU } from '../api/client'
import { useAppStore } from '../stores/app'
import { formatBytes, formatCurrency } from '../utils/format'
import { policyDocumentsFor } from '../utils/siteProfile'
import LegalContentDialog from './LegalContentDialog.vue'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{
  plan: PlanCatalogItem
  skus: PlanSKU[]
  selectedSkuId?: number
  operationLabel?: string
  mode?: 'purchase' | 'renew' | 'change' | 'addon'
  targetName?: string
  loading?: boolean
  error?: string
}>(), {
  selectedSkuId: 0,
  operationLabel: '新购',
  mode: 'purchase',
  targetName: '',
  loading: false,
  error: '',
})

defineEmits<{
  back: []
  'select-sku': [sku: PlanSKU]
  continue: []
}>()

const app = useAppStore()
const profile = computed(() => app.siteProfile)
const selectedSku = computed(() => props.skus.find(item => item.id === props.selectedSkuId) || null)
const purchaseDocuments = computed(() => policyDocumentsFor(profile.value, 'purchase'))
const hasPurchasePolicies = computed(() => purchaseDocuments.value.length > 0)
const activePolicyTitle = ref('')
const activePolicyContent = ref('')
const purchasePoliciesAccepted = ref(false)

watch(() => [props.plan.id, purchaseDocuments.value.map(document => document.slug).join(',')], () => {
  purchasePoliciesAccepted.value = false
})

function openPolicy(title: string, content: string) {
  activePolicyTitle.value = title
  activePolicyContent.value = content
}

function closePolicy() {
  activePolicyTitle.value = ''
  activePolicyContent.value = ''
}

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}`
}
</script>

<style scoped>
.storefront-policy-links { display: grid; gap: 10px; margin: 18px 0 14px; padding: 16px; border: 1px solid var(--warning-border); border-radius: 10px; background: var(--warning-soft); color: var(--text); text-align: left; }
.storefront-policy-links > strong { display: flex; align-items: center; gap: 7px; font-size: 13px; }
.storefront-policy-links > strong :deep(.ui-icon) { color: var(--warning); font-size: 17px; }
.storefront-policy-links p { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.65; }
.storefront-policy-links > div { display: grid; gap: 7px; }
.storefront-policy-links button { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 38px; padding: 9px 11px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); color: var(--primary); font-size: 12px; font-weight: 700; cursor: pointer; text-align: left; }
.storefront-policy-links button:hover { border-color: var(--primary-border); background: var(--primary-soft); }
.storefront-policy-consent { display: flex; align-items: flex-start; gap: 9px; padding-top: 10px; border-top: 1px solid var(--warning-border); color: var(--text); font-size: 12px; font-weight: 700; line-height: 1.5; cursor: pointer; }
</style>
