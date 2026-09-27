<template>
  <ComboboxRoot
    class="ui-autocomplete"
    :model-value="selectedValue"
    :disabled="disabled"
    :ignore-filter="true"
    :open-on-click="true"
    @update:model-value="selectItem"
    @update:open="handleOpenChange"
  >
    <ComboboxAnchor class="ui-autocomplete-anchor">
      <ComboboxInput
        v-bind="inputAttrs"
        v-model="query"
        class="ui-autocomplete-input"
        :id="inputId"
        :placeholder="placeholder"
        :disabled="disabled"
        :display-value="displayValue"
        @update:model-value="updateQuery"
        @keydown.esc="clearUnselected"
        @blur="handleBlur"
      />
      <span v-if="loading" class="ui-button-spinner" aria-label="搜索中" />
      <button v-if="query || selectedValue" type="button" class="ui-autocomplete-clear" aria-label="清除选择" :disabled="disabled" @click="clear">×</button>
      <ComboboxTrigger v-if="dropdown" class="ui-autocomplete-trigger" aria-label="展开建议" :disabled="disabled"><UiIcon name="chevron" /></ComboboxTrigger>
    </ComboboxAnchor>
    <ComboboxPortal>
      <ComboboxContent class="ui-autocomplete-content" position="popper" :side-offset="5">
        <ComboboxViewport>
          <ComboboxItem v-for="(option, index) in suggestions" :key="option.id ?? option.value ?? index" class="ui-autocomplete-option" :value="option">
            <slot name="option" :option="option">{{ displayValue(option) }}</slot>
          </ComboboxItem>
          <div v-if="!suggestions.length && !loading" class="ui-autocomplete-empty">没有匹配结果</div>
        </ComboboxViewport>
        <div v-if="$slots.footer" class="ui-autocomplete-footer"><slot name="footer" /></div>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useAttrs, watch } from 'vue'
import {
  ComboboxAnchor, ComboboxContent, ComboboxInput, ComboboxItem,
  ComboboxPortal, ComboboxRoot, ComboboxTrigger, ComboboxViewport,
} from 'reka-ui'
import UiIcon from './UiIcon.vue'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{
  suggestions?: any[]
  optionLabel?: string
  inputId?: string
  placeholder?: string
  disabled?: boolean
  loading?: boolean
  forceSelection?: boolean
  dropdown?: boolean
  fluid?: boolean
}>(), { suggestions: () => [], optionLabel: 'label', disabled: false, loading: false, forceSelection: false, dropdown: false, fluid: true })
const attrs = useAttrs()
const inputAttrs = computed(() => {
  const { id: _id, ...rest } = attrs
  return rest
})
const model = defineModel<any>()
const selectedValue = computed(() => model.value && typeof model.value === 'object' ? model.value : undefined)
const query = ref('')
const emit = defineEmits<{
  complete: [event: { query: string }]
  'item-select': [event: { value: any }]
  clear: []
}>()
function displayValue(value: any): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  return String(value[props.optionLabel] ?? '')
}
watch(model, value => {
  if (value && typeof value === 'object') query.value = displayValue(value)
  else if (!value) query.value = ''
}, { immediate: true })
function updateQuery(value: string) {
  query.value = value
  if (value !== displayValue(selectedValue.value)) model.value = value
  emit('complete', { query: value })
}
function selectItem(value: any) {
  if (value === undefined || value === null) return
  model.value = value
  query.value = displayValue(value)
  emit('item-select', { value })
}
async function handleOpenChange(open: boolean) {
  if (open || !props.forceSelection) return
  await nextTick()
  clearUnselected()
}
function clearUnselected() {
  if (!props.forceSelection || typeof model.value !== 'string') return
  model.value = null
  query.value = ''
  emit('clear')
}
function handleBlur() {
  window.setTimeout(() => {
    if (document.activeElement?.closest('.ui-autocomplete-content, .ui-autocomplete-anchor')) return
    clearUnselected()
  }, 0)
}
function clear() {
  query.value = ''
  model.value = null
  emit('clear')
}
</script>
