<template>
  <div class="node-lookup">
    <UiAutocomplete
      v-model="selection"
      v-bind="attrs"
      :suggestions="suggestions"
      option-label="name"
      :disabled="disabled"
      :loading="loading"
      :placeholder="placeholder"
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
          <small>{{ option.code }} · {{ option.protocol_endpoint_count || 0 }} 个协议端点</small>
        </div>
      </template>
    </UiAutocomplete>
    <small v-if="loadError" class="node-lookup-error" role="alert">{{ loadError }}</small>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, useAttrs, watch } from 'vue'
import { fetchNodeGroupsPage, type NodeGroupSummary } from '../api/client'
import UiAutocomplete from './UiAutocomplete.vue'

defineOptions({ inheritAttrs: false })
type NodeGroupOption = NodeGroupSummary

const attrs = useAttrs()
const props = withDefaults(defineProps<{ modelValue: number; disabled?: boolean; placeholder?: string; enabledOnly?: boolean }>(), {
  disabled: false,
  placeholder: '输入名称、代码或说明搜索节点组',
  enabledOnly: true,
})
const emit = defineEmits<{ 'update:modelValue': [value: number]; select: [value: NodeGroupOption | null] }>()
const selection = ref<NodeGroupOption | null>(null)
const suggestions = ref<NodeGroupOption[]>([])
const loading = ref(false)
const loadError = ref('')
let requestSequence = 0
let controller: AbortController | null = null

async function load(params: { q?: string; groupId?: number }) {
  controller?.abort()
  controller = new AbortController()
  const currentController = controller
  const sequence = ++requestSequence
  loading.value = true
  loadError.value = ''
  try {
    const page = await fetchNodeGroupsPage({
      q: params.q,
      groupId: params.groupId,
      enabled: params.groupId ? undefined : props.enabledOnly || undefined,
      limit: params.groupId ? 1 : 25,
    }, { signal: currentController.signal })
    if (sequence !== requestSequence) return
    suggestions.value = page.items
    if (params.groupId) {
      selection.value = page.items[0] || null
      emit('select', selection.value)
    }
  } catch {
    if (sequence === requestSequence && !currentController.signal.aborted) {
      suggestions.value = []
      loadError.value = '节点组检索失败，请稍后重试。'
    }
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function search(event: { query: string }) { void load({ q: event.query.trim() }) }
function selectItem(event: { value: NodeGroupOption }) { emit('update:modelValue', event.value.id); emit('select', event.value) }
function clear() { selection.value = null; emit('update:modelValue', 0); emit('select', null) }

watch(() => props.modelValue, id => {
  if (!id) { selection.value = null; return }
  if (selection.value?.id !== id) void load({ groupId: id })
}, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
