<template>
  <article class="storefront-plan-card">
    <header class="storefront-plan-card__header">
      <div>
        <span>{{ plan.slug }}</span>
        <h2>{{ plan.name }}</h2>
      </div>
      <small v-if="offerCount > 0">{{ offerCount }} 个规格</small>
    </header>

    <p class="storefront-plan-card__summary">{{ plan.summary || '稳定、透明的订阅服务。' }}</p>

    <div v-if="loading" class="storefront-plan-card__loading">
      <UiIcon name="refresh" />正在加载价格
    </div>
    <div v-else-if="offer" class="storefront-plan-card__price">
      <strong>{{ formatCurrency(offer.price_cents, offer.currency) }}</strong>
      <span>/ {{ billingLabel(offer) }}</span>
    </div>
    <div v-else class="storefront-plan-card__unavailable">
      <strong>暂不可购买</strong>
      <span>当前没有可用规格</span>
    </div>

    <ul class="storefront-plan-card__features">
      <li><UiIcon name="check" />{{ formatBytes(plan.traffic_bytes) }} 套餐流量</li>
      <li><UiIcon name="check" />{{ plan.speed_limit_mbps > 0 ? `${plan.speed_limit_mbps} Mbps` : '不限速' }}</li>
      <li><UiIcon name="check" />{{ plan.device_limit > 0 ? `${plan.device_limit} 台设备` : '不限设备' }}</li>
    </ul>

    <UiButton type="button" :disabled="loading || (disabled && Boolean(offer))" @click="$emit('select')">
      {{ actionLabel }}<UiIcon name="chevron" />
    </UiButton>
  </article>
</template>

<script setup lang="ts">
import type { PlanCatalogItem, PlanSKU } from '../api/client'
import { formatBytes, formatCurrency } from '../utils/format'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{
  plan: PlanCatalogItem
  offer?: PlanSKU | null
  offerCount?: number
  actionLabel?: string
  loading?: boolean
  disabled?: boolean
}>(), {
  offer: null,
  offerCount: 0,
  actionLabel: '查看详情',
  loading: false,
  disabled: false,
})

defineEmits<{ select: [] }>()

function billingLabel(sku: PlanSKU) {
  const unit = ({ day: '天', month: '个月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit
  if (sku.entitlement_mode === 'traffic_addon') return '一次性流量加购'
  if (sku.billing_unit === 'once') return '永久有效 · 流量用完为止'
  const period = `${sku.billing_value} ${unit}`
  return sku.billing_mode === 'one_time' ? `一次性付费 · ${period}有效` : period
}
</script>
