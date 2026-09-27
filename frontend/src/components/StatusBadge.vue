<template>
  <span class="status-badge" :data-tone="tone">
    <UiIcon class="status-icon" :name="resolvedIcon" />
    <span class="status-label"><slot /></span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiIcon from './UiIcon.vue'
type StatusTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'
const props = withDefaults(defineProps<{ tone?: StatusTone; icon?: string }>(), { tone: 'neutral' })
const resolvedIcon = computed(() => props.icon || ({
  success: 'check', warning: 'alert', danger: 'close', info: 'info', neutral: 'minus',
} satisfies Record<StatusTone, string>)[props.tone])
</script>
