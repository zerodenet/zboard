<template>
  <UiNumberInput
    v-model="displayValue"
    mode="currency"
    :currency="currency"
    :min="minCents === undefined ? undefined : minCents / 100"
    :max="maxCents === undefined ? undefined : maxCents / 100"
    :step="stepCents / 100"
    :min-fraction-digits="2"
    :max-fraction-digits="2"
    use-grouping
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiNumberInput from './UiNumberInput.vue'

const props = withDefaults(defineProps<{
  currency?: string
  minCents?: number
  maxCents?: number
  stepCents?: number
}>(), {
  currency: 'CNY',
  minCents: undefined,
  maxCents: undefined,
  stepCents: 100,
})

const model = defineModel<number>({ default: 0 })
const displayValue = computed({
  get: () => Number(model.value || 0) / 100,
  set: (value: number | null) => { model.value = Math.round(Number(value || 0) * 100) },
})
</script>
