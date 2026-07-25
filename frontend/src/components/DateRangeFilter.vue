<template>
  <div class="date-range-filter" role="group" :aria-label="`${label}（UTC）`" title="按 UTC 自然日筛选，结束日期包含当天">
    <span class="date-range-label">{{ label }}<small>UTC</small></span>
    <UiDateInput
      :model-value="from"
      :max="to || undefined"
      :aria-label="`${label}开始日期`"
      @update:model-value="emit('update:from', $event)"
    />
    <span class="date-range-separator" aria-hidden="true">至</span>
    <UiDateInput
      :model-value="to"
      :min="from || undefined"
      :aria-label="`${label}结束日期`"
      @update:model-value="emit('update:to', $event)"
    />
  </div>
</template>

<script setup lang="ts">
import UiDateInput from './UiDateInput.vue'

defineProps<{ from: string; to: string; label: string }>()
const emit = defineEmits<{
  'update:from': [value: string]
  'update:to': [value: string]
}>()
</script>

<style scoped>
.date-range-filter {
  min-width: 0;
  display: grid;
  grid-template-columns: auto minmax(124px, 1fr) auto minmax(124px, 1fr);
  align-items: center;
  gap: 6px;
}
.date-range-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 700;
  white-space: nowrap;
}
.date-range-label small {
  padding: 1px 4px;
  border: 1px solid var(--line);
  border-radius: 999px;
  color: var(--subtle);
  font-size: 8px;
  letter-spacing: .04em;
}
.date-range-separator {
  color: var(--subtle);
  font-size: 10px;
}
@media (max-width: 430px) {
  .date-range-filter {
    grid-template-columns: auto minmax(0, 1fr) auto minmax(0, 1fr);
  }
  .date-range-label small {
    display: none;
  }
}
</style>
