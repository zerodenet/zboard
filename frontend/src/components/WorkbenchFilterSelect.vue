<template>
  <WorkbenchFilterChip
    :label="label"
    :active="active"
    :value-label="selectedLabel"
    :clearable="clearable"
    @clear="clear"
  >
    <template #default="{ close }">
      <div class="workbench-filter-options" role="listbox" :aria-label="label">
        <UiButton
          v-for="option in selectableOptions"
          :key="String(option.value)"
          variant="ghost"
          size="sm"
          type="button"
          role="option"
          :aria-selected="Object.is(option.value, model)"
          :disabled="option.disabled"
          @click="select(option.value, close)"
        >
          <UiIcon :name="Object.is(option.value, model) ? 'check' : 'plus'" />
          <span>{{ option.label }}</span>
        </UiButton>
      </div>
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { computed, nextTick } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

type FilterOption = { label: string; value: any; disabled?: boolean }

const props = withDefaults(defineProps<{
  label: string
  options: FilterOption[]
  emptyValue?: any
  clearable?: boolean
}>(), {
  emptyValue: '',
  clearable: true,
})

const model = defineModel<any>({ default: '' })
const emit = defineEmits<{ apply: [] }>()

const active = computed(() => !Object.is(model.value, props.emptyValue))
const selectedLabel = computed(() => props.options.find(option => Object.is(option.value, model.value))?.label || String(model.value || ''))
const selectableOptions = computed(() => props.options.filter(option => !Object.is(option.value, props.emptyValue)))

async function commit(value: any, close?: (restoreFocus?: boolean) => void) {
  model.value = value
  await nextTick()
  emit('apply')
  close?.(true)
}

function select(value: any, close: (restoreFocus?: boolean) => void) {
  void commit(value, close)
}

function clear() {
  void commit(props.emptyValue)
}
</script>
