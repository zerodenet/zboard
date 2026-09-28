<template>
  <UiSection :title="title" :description="`本期计费流量前 10 名 · ${comparisonLabel}`">
    <template #meta>
      <UiSelect :model-value="range" :aria-label="`${title}周期`" :options="periodOptions" @update:model-value="emit('update:range', $event as DashboardRange)" />
      <UiButton size="sm" variant="ghost" :loading="loading" :aria-label="`刷新${title}`" @click="emit('refresh')"><UiIcon name="refresh" /></UiButton>
    </template>
    <TransientFeedback :error="error" error-title="排行加载失败" />
    <div v-if="loading && !items.length" class="ranking-empty" role="status">正在加载流量排行…</div>
    <ol v-else-if="items.length" class="ranking-list">
      <li v-for="(item, index) in items" :key="item.id">
        <div class="ranking-heading"><span class="ranking-name"><span class="rank-number">{{ index + 1 }}</span>{{ item.name === '节点' || item.name === '用户' ? `${item.name} #${item.id}` : item.name }}</span><MetricComparison :current="item.traffic_bytes" :previous="item.previous_traffic_bytes" :label="comparisonLabel" :format="formatBytes" /></div>
        <div class="ranking-meter"><div class="ranking-track"><span :style="{ width: `${maximum ? item.traffic_bytes / maximum * 100 : 0}%` }" /></div><span>{{ formatBytes(item.traffic_bytes) }}</span></div>
      </li>
    </ol>
    <div v-else-if="!error" class="ranking-empty">本期暂无流量记录</div>
    <p v-if="asOf" class="ranking-updated">最近更新 <TimeBadge :value="asOf" mode="relative" /></p>
  </UiSection>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { DashboardRange, DashboardTrafficRanking } from '../api/dashboard'
import UiSection from './UiSection.vue'
import UiSelect from './UiSelect.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import TimeBadge from './TimeBadge.vue'
import TransientFeedback from './TransientFeedback.vue'
import MetricComparison from './MetricComparison.vue'
import { formatBytes } from '../utils/format'
const props = defineProps<{ title: string; items: DashboardTrafficRanking[]; comparisonLabel: string; loading: boolean; error: string; range: DashboardRange; asOf?: string }>()
const emit = defineEmits<{ 'update:range': [range: DashboardRange]; refresh: [] }>()
const periodOptions = [{ label: '今天', value: 'today' }, { label: '本月', value: 'month' }, { label: '近 7 天', value: '7d' }, { label: '近 30 天', value: '30d' }]
const maximum = computed(() => Math.max(0, ...props.items.map(item => item.traffic_bytes)))
</script>
<style scoped>
.ranking-updated { margin: 16px 0 0; font-size: 11px; color: var(--muted-foreground); }
:deep(.ui-section-actions) { flex-wrap: wrap; }
:deep(.ui-section-actions .ui-select) { width: 116px; }
.ranking-list { list-style: none; padding: 0; margin: 0; display: grid; gap: 18px; }
.ranking-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.ranking-heading :deep(.comparison-period) { display: none; }
.ranking-name { min-width: 0; overflow-wrap: anywhere; font-size: 13px; font-weight: 500; }
.rank-number { color: var(--muted-foreground); font-size: 11px; display: inline-block; width: 22px; }
.ranking-meter { display: flex; gap: 12px; align-items: center; margin-top: 8px; font-size: 12px; color: var(--muted-foreground); font-variant-numeric: tabular-nums; }
.ranking-track { flex: 1; height: 6px; overflow: hidden; background: var(--surface-subtle); border-radius: 4px; }
.ranking-track span { display: block; height: 100%; background: var(--primary); border-radius: inherit; }
.ranking-empty { color: var(--muted-foreground); padding: 20px 0; font-size: 13px; }
</style>
