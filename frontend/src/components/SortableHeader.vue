<template>
  <th
    :class="headerClasses"
    :aria-sort="ariaSort"
    :data-column-priority="priority"
    scope="col"
  >
    <button
      class="sortable-header-button"
      type="button"
      :disabled="disabled"
      :aria-label="actionLabel"
      @click="$emit('sort', field)"
    >
      <span>{{ label }}</span>
      <UiIcon name="chevron" :class="['sort-indicator', { active, descending: active && direction === 'desc' }]" />
    </button>
  </th>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{
  field: string
  label: string
  sortField?: string
  direction?: 'asc' | 'desc'
  numeric?: boolean
  pinned?: 'start' | 'end'
  priority?: 1 | 2 | 3
  disabled?: boolean
}>(), {
  sortField: '',
  direction: 'asc',
  numeric: false,
  pinned: undefined,
  priority: 1,
  disabled: false,
})

defineEmits<{ sort: [field: string] }>()

const active = computed(() => props.sortField === props.field)
const ariaSort = computed<'ascending' | 'descending' | 'none'>(() => active.value ? (props.direction === 'asc' ? 'ascending' : 'descending') : 'none')
const actionLabel = computed(() => {
  if (props.disabled) return `${props.label}，当前视图排序固定`
  if (!active.value) return `按${props.label}升序排列`
  return `按${props.label}${props.direction === 'asc' ? '降序' : '升序'}排列`
})
const headerClasses = computed(() => [
  'sortable-header',
  { 'numeric-column': props.numeric, 'table-primary-column': props.pinned === 'start', 'table-action-column': props.pinned === 'end' },
])
</script>
