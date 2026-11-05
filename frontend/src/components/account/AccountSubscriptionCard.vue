<template>
        <article class="service-card" :data-selected="selected || undefined" :aria-label="sub.plan_name || `订阅 #${sub.id}`">
          <header class="service-card-heading"><div><slot name="name"><RouterLink class="service-name" :to="subscriptionLocation(sub)">{{ sub.plan_name || `订阅 #${sub.id}` }}</RouterLink></slot><span v-if="sub.sku_name" class="service-period">{{ sub.sku_name }}</span></div><div class="service-expiry"><StatusBadge v-if="sub.status && sub.status !== 'active'" :tone="sub.status === 'expired' ? 'warning' : 'neutral'">{{ stateLabel }}</StatusBadge><span>到期</span><TimeBadge :value="sub.ended_at || sub.end_at" mode="relative" /></div></header>
          <slot name="notice" /><div class="service-quota"><div><span>剩余流量</span><strong>{{ formatBytes(remaining(sub)) }}</strong><small>/ {{ formatBytes(sub.flow_total) }}</small></div><span v-if="remaining(sub) === 0 && (!sub.status || sub.status === 'active')" class="quota-exhausted">本周期流量已用完</span></div>
          <progress class="service-progress" :value="remainingPercent(sub)" max="100" :aria-label="`${sub.plan_name || `订阅 #${sub.id}`}剩余流量`" :aria-valuetext="`${formatBytes(remaining(sub))} / ${formatBytes(sub.flow_total)}`" />
          <div v-if="sub.next_reset_at || sub.renewal_until" class="service-schedule"><span v-if="sub.next_reset_at">下次重置 <TimeBadge :value="sub.next_reset_at" mode="relative" /></span><span v-if="sub.renewal_until">可续费至 <TimeBadge :value="sub.renewal_until" /></span></div><footer class="service-actions"><slot name="actions">
            <UiButton as-child :variant="primary && remaining(sub) > 0 ? 'primary' : 'secondary'" size="sm"><RouterLink :to="subscriptionLocation(sub)"><UiIcon name="key" />{{ remaining(sub) > 0 ? '获取订阅' : '管理订阅' }}</RouterLink></UiButton>
            <UiButton v-if="sub.can_renew" as-child variant="secondary" size="sm"><RouterLink :to="`/account/plans?operation=renew&subscription=${sub.id}`">续费</RouterLink></UiButton>
            <UiButton v-if="remaining(sub) === 0 && sub.can_reset" as-child variant="secondary" size="sm"><RouterLink :to="`/account/plans?operation=reset&subscription=${sub.id}`">购买流量重置</RouterLink></UiButton>
          </slot></footer>
        </article>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { AdminSubscriptionListItem } from '../../api/client'
import { formatBytes, formatUnknownValue } from '../../utils/format'
import UiButton from '../UiButton.vue'
import UiIcon from '../UiIcon.vue'
import StatusBadge from '../StatusBadge.vue'
import TimeBadge from '../TimeBadge.vue'
const props = defineProps<{ subscription: AdminSubscriptionListItem; selected?: boolean; primary?: boolean }>()
const sub = computed(() => props.subscription)
const stateLabel = computed(() => sub.value.end_reason === 'exhausted' ? '已用完' : ({ expired: '已到期', exhausted: '流量耗尽', canceled: '已取消', inactive: '无效' } as Record<string, string>)[sub.value.status] || formatUnknownValue('状态', sub.value.status))
function remaining(row: AdminSubscriptionListItem) { return Math.max(0, (row.flow_total || 0) - (row.flow_used || 0)) }
function remainingPercent(row: AdminSubscriptionListItem) { return row.flow_total > 0 ? Math.min(100, Math.round(remaining(row) / row.flow_total * 1000) / 10) : 0 }
function subscriptionLocation(row: AdminSubscriptionListItem) { return { path: '/account/subscription', query: { subscription: String(row.id) } } }
</script>
<style scoped>
.service-card { min-width: 0; min-height: 176px; display: grid; align-content: start; gap: 14px; padding: 20px; border: 1px solid transparent; border-radius: var(--radius-md); background: var(--surface-soft); }
.service-card-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.service-card-heading > div:first-child { min-width: 0; display: grid; gap: 5px; }
.service-name { overflow-wrap: anywhere; color: var(--foreground); font-size: 14px; font-weight: 600; line-height: 1.4; text-decoration: none; }
.service-name:hover { text-decoration: underline; text-underline-offset: 3px; }
.service-period, .service-expiry > span { color: var(--muted-foreground); font-size: 12px; }
.service-expiry { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 6px; }
.service-quota { display: flex; align-items: baseline; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.service-quota > div { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px; }
.service-quota > div > span { width: 100%; color: var(--muted-foreground); font-size: 12px; }
.service-quota strong { color: var(--foreground); font-size: 24px; font-weight: 600; letter-spacing: -.03em; line-height: 1.2; }
.service-quota small { color: var(--muted-foreground); font-size: 12px; }
.service-progress { display: block; appearance: none; width: 100%; height: 5px; overflow: hidden; border: 0; border-radius: 999px; background: var(--muted-surface); color: var(--primary); }
.service-progress::-webkit-progress-bar { background: var(--muted-surface); border-radius: 999px; }
.service-progress::-webkit-progress-value { background: var(--primary); border-radius: 999px; }
.service-progress::-moz-progress-bar { background: var(--primary); border-radius: 999px; }
.quota-exhausted { color: var(--destructive); font-size: 12px; }
.service-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }

.service-card[data-selected] { border-color: var(--primary); }
.service-schedule { display: flex; flex-wrap: wrap; gap: 8px 16px; color: var(--muted-foreground); font-size: 12px; }
.service-name:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; }
@media (min-width: 900px) {
  .service-card:only-child:not(:has(.service-schedule)):not(:has(.subscription-renewal-result)) { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; min-height: 144px; align-items: center; gap: 14px 32px; }
  .service-card:only-child:not(:has(.service-schedule)):not(:has(.subscription-renewal-result)) .service-card-heading { grid-column: 1; grid-row: 1 / 3; flex-direction: column; gap: 12px; }
  .service-card:only-child:not(:has(.service-schedule)):not(:has(.subscription-renewal-result)) .service-quota { grid-column: 2; grid-row: 1; }
  .service-card:only-child:not(:has(.service-schedule)):not(:has(.subscription-renewal-result)) .service-progress { grid-column: 2; grid-row: 2; }
  .service-card:only-child:not(:has(.service-schedule)):not(:has(.subscription-renewal-result)) .service-actions { grid-column: 3; grid-row: 1 / 3; }
}

@media (max-width: 600px) {
  .service-card { padding: 18px; gap: 14px; }
  .service-name { font-size: 15px; }
  .service-card-heading { flex-wrap: wrap; gap: 10px; }
  .service-actions :deep(.ui-button) { min-height: 40px; }
}
</style>
