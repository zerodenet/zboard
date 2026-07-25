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
        <div class="lookup-option"><strong>{{ option.name }}</strong><small>{{ option.region || '未设置区域' }} · {{ option.address || '未设置地址' }}</small></div>
      </template>
    </UiAutocomplete>
    <small v-if="loadError" class="node-lookup-error" role="alert">{{ loadError }}</small>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, useAttrs, watch } from 'vue'
import { fetchNodesPage, type AdminNodeListItem } from '../api/client'
import UiAutocomplete from './UiAutocomplete.vue'

defineOptions({ inheritAttrs: false })
const attrs = useAttrs()
const props = withDefaults(defineProps<{ modelValue: number; disabled?: boolean; placeholder?: string }>(), { disabled: false, placeholder: '输入名称、区域或地址搜索节点' })
const emit = defineEmits<{ 'update:modelValue': [value: number]; select: [value: AdminNodeListItem | null] }>()
const selection = ref<AdminNodeListItem | null>(null)
const suggestions = ref<AdminNodeListItem[]>([])
const loading = ref(false)
const loadError = ref('')
let requestSequence = 0
let controller: AbortController | null = null

async function load(params: { q?: string; nodeId?: number }) {
  controller?.abort()
  controller = new AbortController()
  const currentController = controller
  const sequence = ++requestSequence
  loading.value = true
  loadError.value = ''
  try {
    const page = await fetchNodesPage({ ...params, limit: params.nodeId ? 1 : 25, sort: 'name', direction: 'asc' }, { signal: currentController.signal })
    if (sequence !== requestSequence) return
    suggestions.value = page.items
    if (params.nodeId) {
      selection.value = page.items[0] || null
      emit('select', selection.value)
    }
  } catch {
    if (sequence === requestSequence && !currentController.signal.aborted) {
      suggestions.value = []
      loadError.value = '节点检索失败，请稍后重试。'
    }
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}
function search(event: { query: string }) { void load({ q: event.query.trim() }) }
function selectItem(event: { value: AdminNodeListItem }) { emit('update:modelValue', event.value.id); emit('select', event.value) }
function clear() { selection.value = null; emit('update:modelValue', 0); emit('select', null) }
watch(() => props.modelValue, id => { if (!id) selection.value = null; else if (selection.value?.id !== id) void load({ nodeId: id }) }, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
