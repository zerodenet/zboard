<template>
  <div class="amount-presets" role="group" aria-label="快捷充值金额">
    <UiButton v-for="value in values" :key="value" variant="secondary" size="sm" :aria-pressed="modelValue === String(value)" @click="$emit('update:modelValue', String(value))">{{ value }}</UiButton>
  </div>
</template>
<script setup lang="ts">
import UiButton from './UiButton.vue'
withDefaults(defineProps<{ modelValue: string; values?: number[] }>(), { values: () => [10, 30, 50, 100] })
defineEmits<{ 'update:modelValue': [value: string] }>()
</script>
<style scoped>
.amount-presets { display: flex; flex-wrap: wrap; gap: 8px; }
.amount-presets .ui-button { min-width: 60px; }
.amount-presets .ui-button[aria-pressed='true'] { border-color: var(--primary); background: var(--primary-soft); }
@media (pointer: coarse) { .amount-presets .ui-button { min-height: 40px; } }
</style>
