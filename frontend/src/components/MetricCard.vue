<template>
  <OverviewCard class="metric-card" :label="label" :value="value" :icon="icon" :tone="iconTone" :description="meta">
    <template #description><span class="metric-meta"><slot name="meta">{{ meta }}</slot></span></template>
    <template #badge><StatusBadge :tone="tone">{{ status }}</StatusBadge></template>
  </OverviewCard>
</template>

<script setup lang="ts">
import OverviewCard from './OverviewCard.vue'
import StatusBadge from './StatusBadge.vue'

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

withDefaults(defineProps<{
  label: string
  value: string | number
  icon: string
  status: string
  meta?: string
  tone?: Tone
  iconTone?: Tone
}>(), { meta: '', tone: 'neutral', iconTone: 'info' })
</script>
