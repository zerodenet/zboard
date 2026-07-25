<template>
  <PrimeInputNumber
    v-model="model"
    v-bind="rootAttrs"
    :input-id="inputId"
    :required="required"
    :aria-label="ariaLabel"
    :aria-labelledby="ariaLabelledby"
    :invalid="invalid"
    :pt="passThrough"
    :locale="locale"
    :mode="mode"
    :currency="currency"
    :currency-display="currencyDisplay"
    :use-grouping="useGrouping"
    :min="min"
    :max="max"
    :step="step"
    :min-fraction-digits="minFractionDigits"
    :max-fraction-digits="maxFractionDigits"
    :prefix="prefix"
    :suffix="suffix"
    fluid
  />
</template>

<script setup lang="ts">
import { computed, useAttrs } from 'vue'
import PrimeInputNumber from 'primevue/inputnumber'

defineOptions({ inheritAttrs: false })

withDefaults(defineProps<{
  locale?: string
  mode?: 'decimal' | 'currency'
  currency?: string
  currencyDisplay?: 'symbol' | 'code' | 'name'
  useGrouping?: boolean
  min?: number
  max?: number
  step?: number
  minFractionDigits?: number
  maxFractionDigits?: number
  prefix?: string
  suffix?: string
}>(), {
  locale: 'zh-CN',
  mode: 'decimal',
  currency: undefined,
  currencyDisplay: 'symbol',
  useGrouping: false,
  min: undefined,
  max: undefined,
  step: 1,
  minFractionDigits: 0,
  maxFractionDigits: 0,
  prefix: undefined,
  suffix: undefined,
})

const model = defineModel<number | null>({ default: null })
const attrs = useAttrs()
const inputId = computed(() => typeof attrs.id === 'string' ? attrs.id : undefined)
const required = computed(() => attrs.required !== undefined && attrs.required !== false)
const ariaLabel = computed(() => typeof attrs['aria-label'] === 'string' ? attrs['aria-label'] : undefined)
const ariaLabelledby = computed(() => typeof attrs['aria-labelledby'] === 'string' ? attrs['aria-labelledby'] : undefined)
const invalid = computed(() => attrs['aria-invalid'] === true || attrs['aria-invalid'] === 'true')
const passThrough = computed(() => ({
  pcInputText: {
    root: {
      'aria-describedby': attrs['aria-describedby'],
      'aria-invalid': attrs['aria-invalid'],
    },
  },
}))
const rootAttrs = computed(() => {
  const result = { ...attrs }
  for (const key of ['id', 'required', 'inputmode', 'aria-label', 'aria-labelledby', 'aria-describedby', 'aria-invalid']) delete result[key]
  return result
})
</script>
