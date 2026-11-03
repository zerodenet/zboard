<template>
  <OverviewCard class="metric-card" :label="label" :value="value" :icon="icon" :tone="iconTone" :description="meta" :aria-busy="loading || undefined">
    <template #value><template v-if="loading"><span class="metric-loading-value" aria-hidden="true"><span class="metric-loading-strut">—</span><Skeleton as="span" class="metric-value-skeleton" /></span><span class="sr-only">加载中</span></template><template v-else>{{ value }}</template></template>
    <template v-if="meta || $slots.meta" #description><span class="metric-meta"><slot name="meta">{{ meta }}</slot></span></template>
    <template v-if="status" #badge><StatusBadge :tone="tone">{{ status }}</StatusBadge></template>
  </OverviewCard>
</template>

<script setup lang="ts">
import OverviewCard from './OverviewCard.vue'
import StatusBadge from './StatusBadge.vue'
import { Skeleton } from './ui/skeleton'

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

withDefaults(defineProps<{
  label: string
  value: string | number
  icon: string
  status?: string
  meta?: string
  tone?: Tone
  iconTone?: Tone
  loading?: boolean
}>(), { meta: '', tone: 'neutral', iconTone: 'neutral' })
</script>

<style scoped>
.metric-loading-value { display: inline-block; position: relative; min-width: 72px; }
.metric-loading-strut { visibility: hidden; }
.metric-value-skeleton { position: absolute; left: 0; top: 50%; width: 100%; height: 22px; transform: translateY(-50%); }
</style>
