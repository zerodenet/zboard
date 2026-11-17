<template>
  <section class="data-workbench" :aria-busy="loading" :data-density="density">
    <header v-if="$slots.filters || $slots.actions || $slots.pagination || total !== undefined || showDensity" class="workbench-toolbar">
      <div v-if="$slots.filters" class="workbench-filters"><slot name="filters" /></div>
      <div class="workbench-actions">
        <span class="sr-only" role="status">{{ refreshing ? '正在更新列表' : '' }}</span>
        <span v-if="total !== undefined" class="workbench-total"><strong>{{ total }}</strong><span>条</span></span>
        <UiButton v-if="showDensity" variant="ghost" size="sm" type="button" :aria-pressed="density === 'comfortable'" :aria-label="density === 'compact' ? '切换为舒适行高' : '切换为紧凑行高'" @click="$emit('update:density', density === 'compact' ? 'comfortable' : 'compact')"><UiIcon name="menu" />{{ density === 'compact' ? '紧凑' : '舒适' }}</UiButton>
        <slot name="pagination" />
        <slot name="actions" />
      </div>
    </header>
    <div v-if="hasSelection()" class="workbench-selection"><slot name="selection" /></div>
    <div class="workbench-content"><slot /></div>
    <footer v-if="$slots.footer" class="workbench-footer"><slot name="footer" /></footer>
  </section>
</template>

<script setup lang="ts">
import { Comment, Fragment, Text, useSlots, type VNode } from 'vue'
import UiIcon from './UiIcon.vue'
import UiButton from './UiButton.vue'

withDefaults(defineProps<{ total?: number; loading?: boolean; refreshing?: boolean; density?: 'compact' | 'comfortable'; showDensity?: boolean }>(), { total: undefined, loading: false, refreshing: false, density: 'compact', showDensity: false })
defineEmits<{ 'update:density': [value: 'compact' | 'comfortable'] }>()
const slots = useSlots()
function hasContent(nodes: VNode[]): boolean {
  return nodes.some(node => node.type !== Comment && (node.type === Fragment
    ? Array.isArray(node.children) && hasContent(node.children as VNode[])
    : node.type !== Text || Boolean(String(node.children || '').trim())))
}
function hasSelection() { return hasContent(slots.selection?.() || []) }
</script>
