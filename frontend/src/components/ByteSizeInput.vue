<template>
  <UiNumberInput
    v-model="displayValue"
    :min="minBytes === undefined ? undefined : minBytes / scale"
    :max="maxBytes === undefined ? undefined : maxBytes / scale"
    :step="step"
    :min-fraction-digits="0"
    :max-fraction-digits="maxFractionDigits"
    :suffix="` ${unit}`"
    use-grouping
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiNumberInput from './UiNumberInput.vue'

const props = withDefaults(defineProps<{
  unit?: 'MiB' | 'GiB'
  minBytes?: number
  maxBytes?: number
  step?: number
  maxFractionDigits?: number
}>(), {
  unit: 'GiB',
  minBytes: undefined,
  maxBytes: undefined,
  step: 1,
  maxFractionDigits: 3,
})

const model = defineModel<number>({ default: 0 })
const scale = computed(() => props.unit === 'MiB' ? 1024 ** 2 : 1024 ** 3)
const displayValue = computed({
  get: () => Number(model.value || 0) / scale.value,
  set: (value: number | null) => { model.value = Math.round(Number(value || 0) * scale.value) },
})
</script>
