<template>
  <div class="endpoint-lookup">
    <UiAutocomplete
      v-model="selection"
      v-bind="attrs"
      :suggestions="suggestions"
      option-label="name"
      :disabled="disabled || !nodeId"
      :loading="loading"
      :placeholder="nodeId ? placeholder : '请先选择承载节点'"
      force-selection
      dropdown
      fluid
      @complete="search"
      @item-select="selectItem"
      @clear="clear"
    >
      <template #option="{ option }">
        <div class="lookup-option">
          <strong>{{ option.name }}</strong>
          <small>{{ option.protocol }} · {{ option.address }}:{{ option.public_port || option.port }}</small>
        </div>
      </template>
    </UiAutocomplete>
    <small v-if="loadError" class="node-lookup-error" role="alert">{{ loadError }}</small>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, useAttrs, watch } from 'vue'
import { fetchProtocolEndpointsPage, type ProtocolEndpointListItem } from '../api/client'
import UiAutocomplete from './UiAutocomplete.vue'

defineOptions({ inheritAttrs: false })
const attrs = useAttrs()
const props = withDefaults(defineProps<{
  modelValue: number
  nodeId: number
  excludeId?: number
  disabled?: boolean
  placeholder?: string
}>(), { excludeId: 0, disabled: false, placeholder: '输入名称或入口搜索同节点协议' })
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
const selection = ref<ProtocolEndpointListItem | null>(null)
const suggestions = ref<ProtocolEndpointListItem[]>([])
const loading = ref(false)
const loadError = ref('')
let controller: AbortController | null = null

async function load(params: { q?: string; endpointId?: number }) {
  controller?.abort()
  if (!props.nodeId) { selection.value = null; suggestions.value = []; return }
  controller = new AbortController()
  const current = controller
  loading.value = true
  loadError.value = ''
  try {
    const page = await fetchProtocolEndpointsPage({
      nodeId: props.nodeId,
      q: params.q,
      ids: params.endpointId ? [params.endpointId] : undefined,
      limit: params.endpointId ? 1 : 25,
      sort: 'name',
      direction: 'asc',
    }, { signal: current.signal })
    if (current.signal.aborted) return
    const items = page.items.filter(item => item.id !== props.excludeId)
    suggestions.value = items
    if (params.endpointId) selection.value = items[0] || null
  } catch {
    if (!current.signal.aborted) {
      suggestions.value = []
      loadError.value = '同节点协议检索失败，请稍后重试。'
    }
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}

function search(event: { query: string }) { void load({ q: event.query.trim() }) }
function selectItem(event: { value: ProtocolEndpointListItem }) { emit('update:modelValue', event.value.id) }
function clear() { selection.value = null; emit('update:modelValue', 0) }

watch([() => props.modelValue, () => props.nodeId, () => props.excludeId], ([id, nodeId], [, previousNodeId]) => {
  if (!nodeId) { controller?.abort(); loading.value = false; selection.value = null; suggestions.value = []; return }
  if (nodeId !== previousNodeId && selection.value?.node_id !== nodeId) selection.value = null
  if (id && id !== props.excludeId) void load({ endpointId: id })
  else { selection.value = null; void load({}) }
}, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
