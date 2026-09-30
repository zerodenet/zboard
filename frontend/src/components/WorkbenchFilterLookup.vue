<template>
  <WorkbenchFilterChip :label="label" :active="Boolean(model)" :value-label="selectedName || `#${model}`" icon="filter" wide @clear="clear">
    <template #default="{ close }">
      <NodeLookup v-if="kind === 'node'" :model-value="model" :placeholder="placeholder" :aria-label="label" @update:model-value="value => commit(value, close)" @select="select" />
      <NodeGroupLookup v-else :model-value="model" :placeholder="placeholder" :aria-label="label" :enabled-only="false" @update:model-value="value => commit(value, close)" @select="select" />
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { fetchNodeGroupsPage, fetchNodesPage } from '../api/client'
import NodeLookup from './NodeLookup.vue'
import NodeGroupLookup from './NodeGroupLookup.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

const props = defineProps<{ kind: 'node' | 'group'; label: string; placeholder: string }>()
const model = defineModel<number>({ default: 0 })
const emit = defineEmits<{ apply: [] }>()
const selectedName = ref('')
const selectedNameID = ref(0)

function select(item: { id: number; name: string } | null) {
  selectedName.value = item?.name || ''
  selectedNameID.value = item?.id || 0
}
watch(model, async (id, _previous, onCleanup) => {
  if (!id) { select(null); return }
  if (selectedNameID.value === id) return
  select(null)
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  try {
    const page = props.kind === 'node'
      ? await fetchNodesPage({ nodeId: id, limit: 1 }, { signal: controller.signal })
      : await fetchNodeGroupsPage({ groupId: id, limit: 1 }, { signal: controller.signal })
    if (!controller.signal.aborted && page.items[0]?.id === id) select(page.items[0])
  } catch { /* Keep the ID visible when a saved filter label cannot be loaded. */ }
}, { immediate: true })

async function commit(value: number, close?: (restoreFocus?: boolean) => void) {
  if (model.value === value) return
  model.value = value
  if (!value) select(null)
  await nextTick()
  emit('apply')
  close?.(true)
}
function clear() { void commit(0) }
</script>
