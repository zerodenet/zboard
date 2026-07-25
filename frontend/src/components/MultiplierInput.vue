<template>
  <UiNumberInput
    v-model="displayValue"
    :min="minMilli / 1000"
    :max="maxMilli / 1000"
    :step="stepMilli / 1000"
    :min-fraction-digits="0"
    :max-fraction-digits="3"
    suffix=" ×"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiNumberInput from './UiNumberInput.vue'

const props = withDefaults(defineProps<{
  minMilli?: number
  maxMilli?: number
  stepMilli?: number
}>(), {
  minMilli: 1,
  maxMilli: 100000,
  stepMilli: 1,
})

const model = defineModel<number>({ default: 1000 })
const displayValue = computed({
  get: () => Number(model.value || 0) / 1000,
  set: (value: number | null) => { model.value = Math.round(Number(value || 0) * 1000) },
})
</script>
