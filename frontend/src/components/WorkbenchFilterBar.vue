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
  </div>
</template>

<script setup lang="ts">
import UiIcon from './UiIcon.vue'

withDefaults(defineProps<{
  active?: boolean
  loading?: boolean
  label?: string
  activeCount?: number
}>(), {
  active: false,
  loading: false,
  label: '列表筛选',
  activeCount: 0,
})

defineEmits<{ clear: [] }>()
</script>

<style scoped>
.workbench-filter-active-count { min-width: 0; height: auto; padding: 0 2px; border-radius: 0; color: var(--muted-foreground); background: transparent; font-size: 11px; font-weight: 400; }
.workbench-filter-clear-all { min-height: 28px; display: inline-flex; align-items: center; gap: 5px; padding: 0 3px; border: 0; color: var(--muted-foreground); background: transparent; font-size: 11px; cursor: pointer; }
.workbench-filter-clear-all:hover { color: var(--foreground); }
.workbench-filter-clear-all:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; border-radius: 3px; }
.workbench-filter-clear-all :deep(.ui-icon) { width: 11px; height: 11px; }
</style>
