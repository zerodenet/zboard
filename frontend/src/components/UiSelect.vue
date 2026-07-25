<template>
  <PrimeSelect
    v-bind="forwardedAttrs"
    :model-value="displayValue"
    :options="options"
    option-label="label"
    option-value="value"
    option-disabled="disabled"
    :option-group-label="hasOptionGroups ? 'label' : undefined"
    :option-group-children="hasOptionGroups ? 'options' : undefined"
    :placeholder="selectPlaceholder"
    fluid
    @update:model-value="updateValue"
    @change="handleChange"
  />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, useAttrs } from 'vue'
import PrimeSelect from 'primevue/select'

defineOptions({ inheritAttrs: false })
const instance = getCurrentInstance()
const attrs = useAttrs()
const props = withDefaults(defineProps<{
  options?: Array<{ label: string; value?: any; disabled?: boolean; options?: Array<{ label: string; value: any; disabled?: boolean }> }>
}>(), { options: () => [] })
const [model, modifiers] = defineModel<any>({
  set(value) {
    if (modifiers.number && typeof value === 'string' && value !== '') {
      const number = Number(value)
      if (!Number.isNaN(number)) return number
    }
    return value
  },
})
const hasModelBinding = computed(() => {
  const vnodeProps = instance?.vnode.props || {}
  return Object.prototype.hasOwnProperty.call(vnodeProps, 'modelValue')
    || Object.prototype.hasOwnProperty.call(vnodeProps, 'model-value')
    || Object.prototype.hasOwnProperty.call(vnodeProps, 'onUpdate:modelValue')
})
const hasOptionGroups = computed(() => props.options.some(option => Array.isArray(option.options)))

const selectPlaceholder = computed(() => {
  if (typeof attrs.placeholder === 'string') return attrs.placeholder
  return props.options.find(option => option.value === '' || option.value === null)?.label
})
const displayValue = computed(() => hasModelBinding.value ? model.value : attrs.value)
const forwardedAttrs = computed(() => {
  const { value: _value, onChange: _onChange, ...rest } = attrs
  return rest
})

const emit = defineEmits<{
  change: [event: { originalEvent?: Event; value: any; target: { value: any } }]
}>()

function updateValue(value: any) {
  if (hasModelBinding.value) model.value = value
}

function handleChange(event: { originalEvent?: Event; value: any }) {
  emit('change', { ...event, target: { value: event.value } })
}
</script>
