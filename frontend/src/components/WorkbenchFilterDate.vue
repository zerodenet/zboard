<template>
  <WorkbenchFilterChip
    :label="label"
    :active="active"
    :value-label="displayValue"
    icon="calendar"
    panel-class="workbench-date-popover"
    @open="resetDraft"
    @clear="clear"
  >
    <template #default="{ close }">
      <div class="workbench-filter-form workbench-filter-date-form">
        <slot name="field" :from="draftFrom" :to="draftTo" :set-from="setFrom" :set-to="setTo">
          <DateRangeFilter v-model:from="draftFrom" v-model:to="draftTo" :label="label" :preset-direction="presetDirection" />
        </slot>
        <div class="workbench-filter-form-actions">
          <slot name="actions" :apply="() => apply(close)" :reset="clearDraft">
            <UiButton variant="ghost" size="sm" type="button" @click="clearDraft">清除</UiButton>
            <UiButton size="sm" type="button" :disabled="!canApply" @click="apply(close)">应用</UiButton>
          </slot>
        </div>
      </div>
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import DateRangeFilter from './DateRangeFilter.vue'
import UiButton from './UiButton.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

const props = withDefaults(defineProps<{ label: string; presetDirection?: 'past' | 'future' }>(), { presetDirection: 'past' })
const from = defineModel<string>('from', { default: '' })
const to = defineModel<string>('to', { default: '' })
const draftFrom = ref(from.value)
const draftTo = ref(to.value)
const emit = defineEmits<{ apply: [] }>()

const active = computed(() => Boolean(from.value || to.value))
const displayValue = computed(() => {
  if (from.value && to.value) {
    const today = new Date().toISOString().slice(0, 10)
    if (from.value === today && to.value === today) return '今天'
    const now = new Date(today + 'T00:00:00Z')
    const sevenDayEdge = new Date(now.getTime() + (props.presetDirection === 'future' ? 6 : -6) * 86400000).toISOString().slice(0, 10)
    const thirtyDayEdge = new Date(now.getTime() + (props.presetDirection === 'future' ? 29 : -29) * 86400000).toISOString().slice(0, 10)
    if ((props.presetDirection === 'future' && from.value === today && to.value === sevenDayEdge) || (props.presetDirection === 'past' && to.value === today && from.value === sevenDayEdge)) return props.presetDirection === 'future' ? '未来 7 天' : '最近 7 天'
    if ((props.presetDirection === 'future' && from.value === today && to.value === thirtyDayEdge) || (props.presetDirection === 'past' && to.value === today && from.value === thirtyDayEdge)) return props.presetDirection === 'future' ? '未来 30 天' : '最近 30 天'
    if (from.value.slice(0, 4) === to.value.slice(0, 4)) return from.value.replace(/-/g, '/') + '–' + to.value.slice(5).replace('-', '/')
    return from.value.replace(/-/g, '/') + '–' + to.value.replace(/-/g, '/')
  }
  if (from.value) return `${from.value} 起`
  if (to.value) return `截至 ${to.value}`
  return ''
})
const canApply = computed(() => {
  if (!draftFrom.value && !draftTo.value) return true
  const from = new Date(draftFrom.value + 'T00:00:00Z')
  const to = new Date(draftTo.value + 'T00:00:00Z')
  return /^\d{4}-\d{2}-\d{2}$/.test(draftFrom.value)
    && /^\d{4}-\d{2}-\d{2}$/.test(draftTo.value)
    && !Number.isNaN(from.getTime()) && !Number.isNaN(to.getTime())
    && from.toISOString().slice(0, 10) === draftFrom.value
    && to.toISOString().slice(0, 10) === draftTo.value
    && from <= to && (to.getTime() - from.getTime()) / 86400000 < 366
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
function clearDraft() { draftFrom.value = ''; draftTo.value = '' }
function setFrom(value: string) { draftFrom.value = value }
function setTo(value: string) { draftTo.value = value }

async function apply(close?: (restoreFocus?: boolean) => void) {
  if (!canApply.value) return
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
