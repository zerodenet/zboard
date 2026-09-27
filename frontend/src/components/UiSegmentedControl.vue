<template>
  <RadioGroupRoot
    class="ui-segmented-control"
    orientation="horizontal"
    :aria-label="label"
    :model-value="modelValue"
    :disabled="disabled"
    @update:model-value="$emit('update:modelValue', String($event))"
    @keydown="handleKeydown"
  >
    <RadioGroupItem
      v-for="option in options"
      :key="option.value"
      class="ui-segmented-item"
      :value="option.value"
      :disabled="option.disabled"
    >{{ option.label }}</RadioGroupItem>
  </RadioGroupRoot>
</template>

<script setup lang="ts">
import { RadioGroupItem, RadioGroupRoot } from 'reka-ui'

const props = defineProps<{
  modelValue: string
  options: Array<{ value: string; label: string; disabled?: boolean }>
  label: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

function handleKeydown(event: KeyboardEvent) {
  if (props.disabled || !['ArrowRight', 'ArrowLeft', 'ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  const enabled = props.options.filter(option => !option.disabled)
  if (!enabled.length) return
  const current = enabled.findIndex(option => option.value === props.modelValue)
  let next = current
  if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = enabled.length - 1
  else next = (current + (event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : -1) + enabled.length) % enabled.length
  const value = enabled[next]?.value
  if (value && value !== props.modelValue) emit('update:modelValue', value)
}
</script>

<style scoped>
.ui-segmented-control {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  border-radius: 9px;
  background: var(--surface-neutral);
}
.ui-segmented-item {
  min-height: 32px;
  min-width: 64px;
  padding: 5px 12px;
  border: 1px solid transparent;
  border-radius: 7px;
  color: var(--text-secondary);
  background: transparent;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  transition: background-color .16s ease-out, border-color .16s ease-out, color .16s ease-out, box-shadow .16s ease-out;
}
.ui-segmented-item:hover:not([data-state='checked']):not(:disabled) { color: var(--text-strong); background: var(--surface-hover); }
.ui-segmented-item[data-state='checked'] { border-color: var(--line-soft); color: var(--text-strong); background: var(--surface); box-shadow: 0 1px 2px var(--line-strong); font-weight: 600; }
.ui-segmented-item:focus-visible { outline: 2px solid var(--focus-border); outline-offset: 2px; z-index: 1; }
.ui-segmented-item:disabled { cursor: not-allowed; opacity: .5; }
@media (prefers-reduced-motion: reduce) { .ui-segmented-item { transition: none; } }
</style>
