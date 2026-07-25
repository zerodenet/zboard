<template>
  <PrimeTag class="status-badge" :severity="severity" :data-tone="tone">
    <UiIcon class="status-icon" :name="resolvedIcon" />
    <span class="status-label"><slot /></span>
  </PrimeTag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import PrimeTag from 'primevue/tag'
import UiIcon from './UiIcon.vue'

type StatusTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

const props = withDefaults(defineProps<{ tone?: StatusTone; icon?: string }>(), { tone: 'neutral' })
const severity = computed(() => ({
  success: 'success',
  warning: 'warn',
  danger: 'danger',
  info: 'info',
  neutral: 'secondary',
} as const)[props.tone])
const resolvedIcon = computed(() => props.icon || ({
  success: 'check',
  warning: 'alert',
  danger: 'close',
  info: 'info',
  neutral: 'minus',
} satisfies Record<StatusTone, string>)[props.tone])
</script>
