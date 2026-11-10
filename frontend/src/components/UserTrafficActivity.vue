<template>
  <div class="user-traffic-activity" :aria-busy="loading || undefined">
    <button v-if="error && !item || pending" type="button" class="activity-retry" :disabled="loading" :aria-label="`${userLabel}：${error || '流量统计正在更新'}，点击重试`" @click="$emit('retry')">{{ pending && !error ? '统计更新中' : '重试' }}</button>
    <template v-else-if="item">
      <div class="activity-total-row"><Tooltip v-if="error" :text="error"><button type="button" class="activity-retry activity-retry-icon" :disabled="loading" :aria-label="`${userLabel}：${error}，重试刷新流量统计`" @click="$emit('retry')"><UiIcon name="refresh" /></button></Tooltip><RouterLink class="activity-total" :to="link" :aria-label="`查看 ${userLabel} 近 7 天流量明细，计费消耗 ${formatBytes(Number(item.used_bytes))}`">{{ BigInt(item.used_bytes) === 0n ? '-' : formatBytes(Number(item.used_bytes)) }}</RouterLink></div>
      <div class="activity-days" role="group" :aria-label="`${userLabel} 近 7 天每日计费消耗`">
        <Tooltip v-for="(day,index) in item.days" :key="day.date" :text="dayLabel(day,index)" :open="activeDay === index" :delay-duration="0" @update:open="value => { if (!value && activeDay === index) activeDay = null }">
          <button type="button" class="activity-day" :aria-label="dayLabel(day,index)" @pointermove="event => { if (event.pointerType !== 'touch') activeDay = index }" @pointerleave="activeDay = null" @focus="activeDay = index" @blur="activeDay = null" @click="activeDay = index" @keydown.esc="activeDay = null">
            <span aria-hidden="true" class="activity-square" :data-level="activityLevel(day.used_bytes)" />
          </button>
          <template #content><span class="activity-day-detail"><span>{{ day.date }}{{ index === 6 ? ' · 今天' : '' }}</span><strong>{{ formatBytes(Number(day.used_bytes)) }}</strong></span></template>
        </Tooltip>
      </div>
    </template>
    <div v-else class="activity-skeleton" role="status" :aria-label="`正在加载 ${userLabel} 的近 7 天流量`"><span /><span /></div>
  </div>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import type { UserTrafficActivity, UserTrafficDay } from '../api/userTrafficActivity'
import { formatBytes } from '../utils/format'
import Tooltip from './ui/Tooltip.vue'
import UiIcon from './UiIcon.vue'
const props = defineProps<{ item?: UserTrafficActivity; userLabel: string; link: RouteLocationRaw; loading?: boolean; pending?: boolean; error?: string }>()
defineEmits<{ retry: [] }>()
const activeDay = ref<number | null>(null)
// The same absolute bands across users, independent of their quota or activity peak.
function activityLevel(bytes: string) {
  const value = BigInt(bytes)
  return value === 0n ? 0 : value < 1024n ** 3n ? 1 : value < 10n * 1024n ** 3n ? 2 : value < 50n * 1024n ** 3n ? 3 : 4
}
function dayLabel(day: UserTrafficDay, index: number) { return `${day.date}${index === 6 ? '，今天' : ''}，计费消耗 ${formatBytes(Number(day.used_bytes))}` }
watch(() => [props.userLabel, props.item, props.error, props.pending], () => { activeDay.value = null })
</script>
<style scoped>
.user-traffic-activity { display: grid; justify-items: end; align-content: center; gap: 4px; min-height: 42px; font-variant-numeric: tabular-nums; }
.activity-total-row { display: flex; align-items: center; justify-content: end; gap: 4px; }
.activity-retry-icon { display: grid; place-items: center; width: 18px; height: 24px; flex: none; }
.activity-total { color: var(--foreground); font-size: 13px; font-weight: 500; white-space: nowrap; text-decoration: none; }
.activity-total:hover { text-decoration: underline; text-underline-offset: 3px; }
.activity-days { display: flex; gap: 1px; }
.activity-day { display: grid; place-items: center; width: 16px; height: 20px; border: 0; border-radius: 3px; background: transparent; padding: 0; cursor: pointer; }
.activity-square { width: 10px; height: 10px; border-radius: 2px; background: color-mix(in srgb, var(--foreground) 7%, var(--background)); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--foreground) 8%, transparent); }
.activity-square[data-level="1"] { background: color-mix(in srgb, var(--primary) 22%, var(--background)); }
.activity-square[data-level="2"] { background: color-mix(in srgb, var(--primary) 43%, var(--background)); }
.activity-square[data-level="3"] { background: color-mix(in srgb, var(--primary) 70%, var(--background)); }
.activity-square[data-level="4"] { background: var(--primary); }
.activity-total:focus-visible, .activity-day:focus-visible, .activity-retry:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.activity-day-detail { display: grid; gap: 4px; min-width: 130px; font-size: 12px; }
.activity-day-detail strong { color: var(--foreground); font-size: 13px; }
.activity-retry { padding: 0; border: 0; background: transparent; font: inherit; font-size: 12px; color: var(--muted-foreground); cursor: pointer; }
.activity-retry:hover { color: var(--foreground); text-decoration: underline; }
.activity-skeleton { display: grid; justify-items: end; gap: 7px; }
.activity-skeleton span { display: block; width: 60px; height: 12px; border-radius: 3px; background: color-mix(in srgb, var(--foreground) 7%, var(--background)); }
.activity-skeleton span + span { width: 118px; height: 10px; }
@media (max-width: 600px) {
  .user-traffic-activity { display: flex; justify-content: space-between; align-items: center; gap: 8px; min-height: 24px; }
  .activity-day { width: 24px; height: 28px; }
  .activity-days { gap: 0; }
  .activity-skeleton { display: flex; align-items: center; gap: 12px; }
}
</style>
