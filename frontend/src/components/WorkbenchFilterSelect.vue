<template>
  <WorkbenchFilterChip
    :label="label"
    :active="active"
    :value-label="selectedLabel"
    :clearable="clearable"
    icon="filter"
    compact
    @clear="clear"
  >
    <template #default="{ close }">
      <div class="workbench-filter-options" role="listbox" :aria-label="label" @keydown.down.prevent="focusOption($event, 1)" @keydown.up.prevent="focusOption($event, -1)" @keydown.home.prevent="focusEdge($event, 'first')" @keydown.end.prevent="focusEdge($event, 'last')">
        <button
          v-for="option in selectableOptions"
          :key="String(option.value)"
          class="workbench-filter-option"
          type="button"
          role="option"
          :aria-selected="Object.is(option.value, model)"
          :disabled="option.disabled"
          @click="select(option.value, close)"
        >
          <slot name="option" :option="option" :selected="Object.is(option.value, model)">
            <span class="workbench-filter-option-indicator" aria-hidden="true" />
            <span>{{ option.label }}</span>
          </slot>
        </button>
        <slot v-if="!selectableOptions.length" name="empty">暂无可用选项</slot>
      </div>
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { computed, nextTick } from 'vue'
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

function enabledOptions(event: KeyboardEvent) {
  return Array.from((event.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>('[role="option"]:not(:disabled)'))
}
function focusOption(event: KeyboardEvent, direction: number) {
  const options = enabledOptions(event)
  if (!options.length) return
  const current = options.indexOf(document.activeElement as HTMLButtonElement)
  options[(current + direction + options.length) % options.length]?.focus()
}
function focusEdge(event: KeyboardEvent, edge: 'first' | 'last') {
  const options = enabledOptions(event)
  options[edge === 'first' ? 0 : options.length - 1]?.focus()
}
</script>

<style scoped>
.workbench-filter-option { width: 100%; min-height: 30px; display: flex; align-items: center; gap: 9px; padding: 5px 8px; border: 0; border-radius: 5px; color: var(--foreground); background: transparent; font-size: 12px; line-height: 1.4; text-align: left; cursor: pointer; }
.workbench-filter-option:hover, .workbench-filter-option:focus-visible, .workbench-filter-option[aria-selected='true'] { color: var(--foreground); background: var(--surface-subtle); }
.workbench-filter-option:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
.workbench-filter-option:disabled { opacity: .5; cursor: not-allowed; }
.workbench-filter-option-indicator { width: 12px; height: 12px; flex: none; border: 1px solid var(--input); border-radius: 50%; background: var(--card); }
.workbench-filter-option[aria-selected='true'] .workbench-filter-option-indicator { border: 4px solid var(--primary); }
</style>
