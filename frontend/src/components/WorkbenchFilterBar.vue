<template>
  <div class="workbench-stripe-filters" role="group" :aria-label="label">
    <slot />
    <span v-if="activeCount > 0" class="workbench-filter-active-count" :aria-label="`已启用 ${activeCount} 项筛选`">{{ activeCount }} 项</span>
    <button
      v-if="active"
      class="workbench-filter-clear-all"
      type="button"
      :disabled="loading"
      @click="$emit('clear')"
    >
      <UiIcon name="close" />
      <slot name="clear-label">清除筛选</slot>
    </button>
    <slot name="trailing" />
    <button v-if="$slots.advanced" class="workbench-filter-clear-all" type="button" :aria-expanded="advancedOpen" :aria-controls="advancedID" @click="advancedOpen = !advancedOpen">
      <UiIcon name="filter" />{{ advancedOpen ? '收起更多筛选' : '更多筛选' }}<span v-if="advancedCount">（{{ advancedCount }} 项）</span>
    </button>
    <div v-if="$slots.advanced && advancedOpen" :id="advancedID" class="advanced-filter-panel" role="group" aria-label="更多筛选条件"><slot name="advanced" /></div>
  </div>
</template>

<script setup lang="ts">
import { ref, useId, watch } from 'vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{
  active?: boolean
  loading?: boolean
  label?: string
  advancedCount?: number
  activeCount?: number
}>(), {
  active: false,
  loading: false,
  label: '列表筛选',
  activeCount: 0,
  advancedCount: 0,
})

defineEmits<{ clear: [] }>()
const advancedID = `advanced-filters-${useId()}`
const advancedOpen = ref(props.advancedCount > 0)
watch(() => props.advancedCount, count => { if (count > 0) advancedOpen.value = true })
</script>

<style scoped>
.advanced-filter-panel { flex: 1 0 100%; display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding-top: 8px; }
.workbench-filter-active-count { min-width: 0; height: auto; padding: 0 2px; border-radius: 0; color: var(--muted-foreground); background: transparent; font-size: 11px; font-weight: 400; }
.workbench-filter-clear-all { min-height: 28px; display: inline-flex; align-items: center; gap: 5px; padding: 0 3px; border: 0; color: var(--muted-foreground); background: transparent; font-size: 11px; cursor: pointer; }
.workbench-filter-clear-all:hover { color: var(--foreground); }
.workbench-filter-clear-all:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; border-radius: 3px; }
.workbench-filter-clear-all :deep(.ui-icon) { width: 11px; height: 11px; }
</style>
