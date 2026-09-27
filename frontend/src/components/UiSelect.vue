<template>
  <SelectRoot :model-value="primitiveValue" :disabled="disabled" @update:model-value="updateValue">
    <SelectTrigger v-bind="forwardedAttrs" class="ui-select" :aria-invalid="invalid || undefined">
      <SelectValue :placeholder="selectPlaceholder">{{ selectedLabel }}</SelectValue>
      <SelectIcon class="ui-select-chevron" aria-hidden="true">⌄</SelectIcon>
    </SelectTrigger>
    <SelectPortal>
      <SelectContent class="ui-select-content" position="popper" :side-offset="5">
        <SelectViewport class="ui-select-viewport">
          <template v-for="(option, index) in options" :key="index">
            <SelectGroup v-if="option.options">
              <SelectLabel class="ui-select-group-label">{{ option.label }}</SelectLabel>
              <SelectItem v-for="item in option.options" :key="String(item.value)" class="ui-select-item" :value="toPrimitive(item.value)" :disabled="item.disabled">
                <SelectItemText>{{ item.label }}</SelectItemText>
                <SelectItemIndicator class="ui-select-indicator">✓</SelectItemIndicator>
              </SelectItem>
            </SelectGroup>
            <SelectItem v-else class="ui-select-item" :value="toPrimitive(option.value ?? '')" :disabled="option.disabled">
              <SelectItemText>{{ option.label }}</SelectItemText>
              <SelectItemIndicator class="ui-select-indicator">✓</SelectItemIndicator>
            </SelectItem>
          </template>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, useAttrs } from 'vue'
import {
  SelectContent, SelectGroup, SelectIcon, SelectItem, SelectItemIndicator,
  SelectItemText, SelectLabel, SelectPortal, SelectRoot, SelectTrigger,
  SelectValue, SelectViewport,
} from 'reka-ui'

defineOptions({ inheritAttrs: false })
type Option = { label: string; value?: any; disabled?: boolean; options?: Array<{ label: string; value: any; disabled?: boolean }> }
const props = withDefaults(defineProps<{ options?: Option[]; disabled?: boolean }>(), { options: () => [], disabled: false })
const instance = getCurrentInstance()
const attrs = useAttrs()
const [model, modifiers] = defineModel<any>({
  set(value) {
    if (modifiers.number && typeof value === 'string' && value !== '') {
      const number = Number(value)
      if (!Number.isNaN(number)) return number
    }
    return value
  },
})
const emit = defineEmits<{ change: [event: { value: any; target: { value: any } }] }>()
const hasModelBinding = computed(() => {
  const vnodeProps = instance?.vnode.props || {}
  return 'modelValue' in vnodeProps || 'model-value' in vnodeProps || 'onUpdate:modelValue' in vnodeProps
})
const displayValue = computed(() => hasModelBinding.value ? model.value : attrs.value)
const emptyValue = '__zboard_empty_selection__'
const primitiveValue = computed(() => toPrimitive(displayValue.value))
function toPrimitive(value: any) { return value === '' || value === null ? emptyValue : value }
const forwardedAttrs = computed(() => {
  const { value: _value, onChange: _onChange, placeholder: _placeholder, disabled: _disabled, ...rest } = attrs
  return rest
})
const selectPlaceholder = computed(() => typeof attrs.placeholder === 'string'
  ? attrs.placeholder
  : props.options.find(option => option.value === '' || option.value === null)?.label || '请选择')
const selectedLabel = computed(() => {
  const all: Array<{ label: string; value?: any }> = []
  for (const option of props.options) {
    if (option.options) all.push(...option.options)
    else all.push(option)
  }
  return all.find(option => option.value === displayValue.value)?.label || ''
})
const invalid = computed(() => attrs['aria-invalid'] === true || attrs['aria-invalid'] === 'true')
function updateValue(value: any) {
  if (value === emptyValue) value = ''
  if (hasModelBinding.value) model.value = value
  emit('change', { value, target: { value } })
}
</script>
