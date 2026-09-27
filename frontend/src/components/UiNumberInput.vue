<template>
  <span class="ui-number-field" :class="{ 'ui-number-field-disabled': disabled }">
    <span v-if="prefix || currencyPrefix" class="ui-number-affix">{{ prefix || currencyPrefix }}</span>
    <input
      v-bind="attrs"
      class="ui-input ui-number-input"
      type="text"
      :value="displayValue"
      :disabled="disabled"
      :inputmode="maxFractionDigits === 0 ? 'numeric' : 'decimal'"
      :aria-valuemin="min"
      :aria-valuemax="max"
      @focus="startEdit"
      @input="updateInput"
      @blur="finishEdit"
    />
    <span v-if="suffix" class="ui-number-affix">{{ suffix }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed, ref, useAttrs, watch } from 'vue'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{
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
  disabled?: boolean
}>(), {
  locale: 'zh-CN', mode: 'decimal', currency: undefined, currencyDisplay: 'symbol',
  useGrouping: false, min: undefined, max: undefined, step: 1,
  minFractionDigits: 0, maxFractionDigits: 0, prefix: undefined, suffix: undefined, disabled: false,
})
const model = defineModel<number | null>({ default: null })
const attrs = useAttrs()
const editing = ref(false)
const draft = ref('')
const currencyPrefix = computed(() => props.mode === 'currency' && props.currency
  ? new Intl.NumberFormat(props.locale, { style: 'currency', currency: props.currency, currencyDisplay: props.currencyDisplay }).formatToParts(0).find(part => part.type === 'currency')?.value
  : '')
const formatter = computed(() => new Intl.NumberFormat(props.locale, {
  useGrouping: props.useGrouping,
  minimumFractionDigits: props.minFractionDigits,
  maximumFractionDigits: props.maxFractionDigits,
}))
const displayValue = computed(() => editing.value ? draft.value : model.value === null || model.value === undefined ? '' : formatter.value.format(model.value))
watch(model, value => { if (!editing.value) draft.value = value == null ? '' : String(value) })
function startEdit() {
  editing.value = true
  draft.value = model.value === null || model.value === undefined ? '' : String(model.value)
}
function updateInput(event: Event) {
  draft.value = (event.target as HTMLInputElement).value
  const normalized = draft.value.replace(/[,\s，]/g, '')
  if (normalized === '') { model.value = null; return }
  if (!/^-?(?:\d+\.?\d*|\.\d+)$/.test(normalized)) return
  const value = Number(normalized)
  if (Number.isFinite(value)) model.value = value
}
function finishEdit() {
  const raw = Number(draft.value.replace(/[,\s，]/g, ''))
  if (draft.value.trim() && Number.isFinite(raw)) {
    let value = raw
    if (props.min !== undefined) value = Math.max(props.min, value)
    if (props.max !== undefined) value = Math.min(props.max, value)
    model.value = Number(value.toFixed(props.maxFractionDigits))
  }
  editing.value = false
}
</script>
