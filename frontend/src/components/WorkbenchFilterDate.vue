<template>
  <WorkbenchFilterChip
    :label="label"
    :active="active"
    :value-label="displayValue"
    wide
    @open="resetDraft"
    @clear="clear"
  >
    <template #default="{ close }">
      <div class="workbench-filter-form workbench-filter-date-form">
        <DateRangeFilter v-model:from="draftFrom" v-model:to="draftTo" :label="label" />
        <UiButton size="sm" type="button" @click="apply(close)"><UiIcon name="check" />应用</UiButton>
      </div>
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import DateRangeFilter from './DateRangeFilter.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

const props = defineProps<{ label: string }>()
const from = defineModel<string>('from', { default: '' })
const to = defineModel<string>('to', { default: '' })
const draftFrom = ref(from.value)
const draftTo = ref(to.value)
const emit = defineEmits<{ apply: [] }>()

const active = computed(() => Boolean(from.value || to.value))
const displayValue = computed(() => {
  if (from.value && to.value) return `${from.value} – ${to.value}`
  if (from.value) return `${from.value} 起`
  if (to.value) return `截至 ${to.value}`
  return ''
})

watch(from, value => {
  draftFrom.value = value
})
watch(to, value => {
  draftTo.value = value
})

function resetDraft() {
  draftFrom.value = from.value
  draftTo.value = to.value
}

async function apply(close?: (restoreFocus?: boolean) => void) {
  from.value = draftFrom.value
  to.value = draftTo.value
  await nextTick()
  emit('apply')
  close?.(true)
}

async function clear() {
  from.value = ''
  to.value = ''
  draftFrom.value = ''
  draftTo.value = ''
  await nextTick()
  emit('apply')
}
</script>
