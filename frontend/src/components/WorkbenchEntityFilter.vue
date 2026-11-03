<template>
  <WorkbenchFilterChip :label="label" :active="Boolean(modelValue)" :value-label="selected?.name || `#${modelValue}`" icon="filter" wide @clear="clear" @close="restoreSelection">
    <template #default="{ close }">
      <UiAutocomplete v-model="selection" :suggestions="suggestions" option-label="name" :aria-label="`搜索${label}`" :placeholder="`按名称搜索${label}`" :loading="loading" force-selection dropdown @open="openList" @complete="event => search(event.query)" @item-select="event => choose(event.value, close)" @clear="clearSelection">
        <template #option="{ option }"><span class="entity-filter-option"><strong>{{ option.name }}</strong><small>{{ option.secondary }}</small></span></template>
        <template #footer><span class="entity-filter-help">最多显示 20 项，请输入关键词缩小范围。</span></template>
      </UiAutocomplete>
      <p v-if="error" role="alert" class="entity-filter-error">{{ error }}</p>
    </template>
  </WorkbenchFilterChip>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { fetchNodesPage, fetchUsersPage, fetchSubscriptionsPage, fetchProtocolEndpointsPage } from '../api/client'
import { fetchAdminEntityReferences } from '../api/readModels'
import { optionalQueryID } from '../api/queryID'
import UiAutocomplete from './UiAutocomplete.vue'
import WorkbenchFilterChip from './WorkbenchFilterChip.vue'

type Option = { id: number; name: string; secondary: string }
const props = defineProps<{ modelValue: string; kind: 'user' | 'node' | 'subscription' | 'protocol_endpoint'; label: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; apply: [] }>()
const selected = ref<Option | null>(null)
const selection = ref<Option | string | null>(null)
const suggestions = ref<Option[]>([])
const loading = ref(false), error = ref('')
let searchController: AbortController | undefined
let searchGeneration = 0

function openList() {
  // Typing also opens Reka's list. Preserve that query; a selected item's label
  // is display text and should still open the default list without a keyword.
  void search(typeof selection.value === 'string' ? selection.value : '')
}
async function search(query: string) {
  searchController?.abort()
  const controller = new AbortController(), generation = ++searchGeneration
  searchController = controller
  loading.value = true; error.value = ''; suggestions.value = []
  const params = { q: query.trim() || undefined, offset: 0, limit: 20 }
  try {
    const page = props.kind === 'node' ? await fetchNodesPage(params, { signal: controller.signal })
      : props.kind === 'user' ? await fetchUsersPage(params, { signal: controller.signal })
      : props.kind === 'subscription' ? await fetchSubscriptionsPage(params, { signal: controller.signal })
      : await fetchProtocolEndpointsPage(params, { signal: controller.signal })
    if (controller.signal.aborted || generation !== searchGeneration) return
    suggestions.value = page.items.map((item: any) => ({ id: item.id,
      name: props.kind === 'user' ? item.email : props.kind === 'subscription' ? `${item.user_email || '订阅'} · ${item.plan_name || '未命名套餐'}` : item.name,
      secondary: props.kind === 'subscription' ? `#${item.id} · ${item.sku_name || ''}` : `#${item.id} · ${item.region || item.protocol || item.account_name || ''}`,
    }))
  } catch (cause: any) {
    if (!controller.signal.aborted && generation === searchGeneration) error.value = cause?.response?.data?.message || `${props.label}搜索失败，请重新搜索。`
  } finally { if (generation === searchGeneration) loading.value = false }
}
function choose(item: Option, close: (restoreFocus?: boolean) => void) {
  selected.value = item; selection.value = item
  emit('update:modelValue', String(item.id)); emit('apply'); close(true)
}
function clear() {
  selected.value = null; selection.value = null
  emit('update:modelValue', ''); emit('apply')
}
function restoreSelection() {
  if (typeof selection.value !== 'string' && selection.value) return
  selection.value = props.modelValue ? selected.value || { id: Number(props.modelValue), name: `#${props.modelValue}`, secondary: '' } : null
}
function clearSelection(reason?: 'explicit' | 'unselected') {
  if (reason === 'unselected') restoreSelection()
  else clear()
}
watch(() => [props.kind, props.modelValue] as const, async ([kind, id], previous, onCleanup) => {
  if (kind !== previous?.[0]) { searchController?.abort(); searchGeneration++; loading.value = false; suggestions.value = [] }
  if (kind === previous?.[0] && String(selected.value?.id) === id) return
  selected.value = null; selection.value = id ? { id: Number(id), name: `#${id}`, secondary: '' } : null
  if (!id) return
  try { optionalQueryID(id, props.label) } catch { return }
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  try {
    const refs = await fetchAdminEntityReferences({ targets: [`${kind}:${id}`], signal: controller.signal })
    if (controller.signal.aborted) return
    const item = refs.targets[`${kind}:${id}`]
    if (item && !item.missing) selected.value = selection.value = { id: item.id, name: item.display_name, secondary: item.secondary || '' }
  } catch { /* A saved or historical ID remains visible and is never silently cleared. */ }
}, { immediate: true })
onBeforeUnmount(() => searchController?.abort())
</script>
<style scoped>
.entity-filter-option { display: grid; gap: 3px; min-width: 0; }
.entity-filter-option strong { font-size: 13px; font-weight: 500; overflow-wrap: anywhere; }
.entity-filter-option small, .entity-filter-help { font-size: 12px; color: var(--muted-foreground); }
.entity-filter-help { display: block; padding: 8px; }
.entity-filter-error { font-size: 12px; color: var(--destructive); }
</style>
