<template>
  <div class="node-lookup">
    <UiAutocomplete
      v-model="selection"
      v-bind="attrs"
      :input-id="typeof attrs.id === 'string' ? attrs.id : undefined"
      :suggestions="suggestions"
      option-label="name"
      :disabled="disabled"
      :loading="listLoading || selectionLoading"
      :placeholder="placeholder"
      force-selection
      dropdown
      fluid
      @open="openList"
      @complete="search"
      @item-select="selectItem"
      @clear="clear"
    >
      <template #option="{ option }">
        <div class="lookup-option"><strong>{{ option.name }}</strong><small>{{ option.region || '未设置区域' }} · {{ option.address || '未设置地址' }}</small></div>
      </template>
      <template v-if="total > pageSize || loadError" #footer>
        <div class="node-lookup-footer">
          <small>已显示 {{ suggestions.length }} / {{ total }} 个节点</small>
          <UiButton v-if="loadError" variant="ghost" size="sm" :loading="listLoading" @click="loadList(false)">重试</UiButton>
          <UiButton v-else-if="hasMore" variant="ghost" size="sm" :loading="listLoading" @click="loadList(true)">加载更多</UiButton>
        </div>
      </template>
    </UiAutocomplete>
    <small v-if="loadError" class="node-lookup-error" role="alert">{{ loadError }}</small>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useAttrs, watch } from 'vue'
import { fetchNodesPage, type AdminNodeListItem } from '../api/client'
import UiAutocomplete from './UiAutocomplete.vue'
import UiButton from './UiButton.vue'

defineOptions({ inheritAttrs: false })
const attrs = useAttrs()
const props = withDefaults(defineProps<{ modelValue: number; disabled?: boolean; placeholder?: string }>(), { disabled: false, placeholder: '选择节点，或输入名称、区域、地址搜索' })
const emit = defineEmits<{ 'update:modelValue': [value: number]; select: [value: AdminNodeListItem | null] }>()
const selection = ref<AdminNodeListItem | string | null>(null)
const suggestions = ref<AdminNodeListItem[]>([])
const listLoading = ref(false)
const selectionLoading = ref(false)
const loadError = ref('')
const query = ref('')
const total = ref(0)
const pageSize = 25
const nextOffset = ref(0)
const hasMore = computed(() => nextOffset.value < total.value)
let listSequence = 0
let listController: AbortController | undefined
let selectionController: AbortController | undefined

async function loadList(append = false) {
  if (append && (listLoading.value || !hasMore.value)) return
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  const sequence = ++listSequence
  const offset = append ? nextOffset.value : 0
  listLoading.value = true
  loadError.value = ''
  if (!append) { suggestions.value = []; total.value = 0; nextOffset.value = 0 }
  try {
    const page = await fetchNodesPage({ q: query.value || undefined, offset, limit: pageSize, sort: 'name', direction: 'asc' }, { signal: controller.signal })
    if (sequence !== listSequence || controller.signal.aborted) return
    const items = append ? [...suggestions.value, ...page.items] : page.items
    suggestions.value = [...new Map(items.map(node => [node.id, node])).values()]
    total.value = page.page.total
    nextOffset.value = page.items.length ? offset + page.items.length : total.value
  } catch {
    if (sequence !== listSequence || controller.signal.aborted) return
    loadError.value = '节点列表加载失败，请重试。'
  } finally {
    if (sequence === listSequence) listLoading.value = false
  }
}

// Hydrating the selected ID must not restrict the list or cancel browsing.
async function loadSelection(id: number) {
  selectionController?.abort()
  const controller = new AbortController()
  selectionController = controller
  selectionLoading.value = true
  try {
    const page = await fetchNodesPage({ nodeId: id, limit: 1 }, { signal: controller.signal })
    if (controller.signal.aborted || props.modelValue !== id) return
    const node = page.items[0] || null
    if (typeof selection.value !== 'string') selection.value = node
    emit('select', node)
  } catch {
    if (!controller.signal.aborted) loadError.value = '当前节点加载失败，请重新选择节点。'
  } finally {
    if (selectionController === controller) selectionLoading.value = false
  }
}
function openList() {
  query.value = typeof selection.value === 'string' ? selection.value.trim() : ''
  void loadList()
}
function search(event: { query: string }) { query.value = event.query.trim(); void loadList() }
function selectItem(event: { value: AdminNodeListItem }) { emit('update:modelValue', event.value.id); emit('select', event.value) }
function clear() {
  selectionController?.abort()
  selectionLoading.value = false
  selection.value = null
  query.value = ''
  emit('update:modelValue', 0)
  emit('select', null)
  void loadList()
}
watch(() => props.modelValue, id => {
  selectionController?.abort()
  selectionLoading.value = false
  if (!id) selection.value = null
  else if (typeof selection.value !== 'object' || selection.value?.id !== id) void loadSelection(id)
}, { immediate: true })
onBeforeUnmount(() => { listController?.abort(); selectionController?.abort() })
</script>

<style scoped>
.node-lookup-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.node-lookup-footer small { color: var(--muted-foreground); }
</style>
