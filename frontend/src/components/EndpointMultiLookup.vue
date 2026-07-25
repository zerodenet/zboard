<template>
  <div class="endpoint-lookup" role="group" tabindex="-1">
    <div class="lookup-search"><UiIcon name="search" /><UiInput v-model.trim="search" :placeholder="placeholder" aria-label="搜索协议端点" maxlength="100" /></div>
    <p v-if="error" class="lookup-error" role="alert"><UiIcon name="alert" />{{ error }}</p>
    <div class="lookup-scope">
      <span aria-live="polite">{{ loading ? '正在解析筛选范围…' : `当前筛选 ${resultTotal} 个可用端点` }}</span>
      <div class="lookup-scope-actions">
        <UiButton variant="secondary" size="sm" type="button" :loading="bulkAction === 'add'" :disabled="loading || Boolean(bulkAction) || resultTotal === 0" @click="applyFilteredSelection('add')"><UiIcon name="plus" />全部加入</UiButton>
        <UiButton variant="ghost" size="sm" type="button" :loading="bulkAction === 'remove'" :disabled="loading || Boolean(bulkAction) || resultTotal === 0" @click="applyFilteredSelection('remove')"><UiIcon name="minus" />从待保存成员移除</UiButton>
      </div>
    </div>
    <p v-if="bulkMessage" class="lookup-draft-status" role="status"><UiIcon name="info" />{{ bulkMessage }}</p>
    <div class="lookup-results" :aria-busy="loading">
      <label v-for="item in results" :key="item.id" :class="{ selected: selectedIDs.has(item.id) }">
        <UiCheckbox :model-value="selectedIDs.has(item.id)" @update:model-value="toggle(item.id, Boolean($event))" />
        <span><strong>{{ item.name }}</strong><small>{{ item.node_name || `节点 #${item.node_id}` }} · {{ item.protocol.toUpperCase() }} · {{ item.address }}:{{ item.public_port || item.port }}</small></span>
        <em>{{ formatMultiplier(item.multiplier_milli) }}</em>
      </label>
      <p v-if="!loading && !results.length" class="lookup-empty">{{ search ? '没有匹配的可用端点。' : '暂无可用端点。' }}</p>
      <p v-if="loading" class="lookup-empty">加载中…</p>
    </div>
    <section class="lookup-selected">
      <header><strong>已选 {{ modelValue.length }} 个端点</strong><small>搜索列表最多展示 25 条；“全部加入/移除”按一次服务端筛选快照处理，已选项每页 50 条</small></header>
      <div v-if="modelValue.length" class="selection-chips"><span v-for="id in visibleSelectedIDs" :key="id">{{ selectedLabel(id) }}<UiButton variant="ghost" size="sm" icon type="button" :aria-label="`移除端点 ${id}`" @click="toggle(id, false)"><UiIcon name="close" /></UiButton></span></div>
      <p v-else>请从上方搜索结果中选择端点。</p>
      <nav v-if="selectedPageCount > 1" class="selected-pagination" aria-label="已选协议端点分页">
        <span aria-live="polite">第 {{ selectedStart + 1 }}–{{ selectedEnd }} 项，共 {{ modelValue.length }} 项</span>
        <div><UiButton variant="ghost" size="sm" type="button" :disabled="selectedPage <= 1" @click="changeSelectedPage(-1)">上一页</UiButton><UiButton variant="ghost" size="sm" type="button" :disabled="selectedPage >= selectedPageCount" @click="changeSelectedPage(1)">下一页</UiButton></div>
      </nav>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchProtocolEndpointSelection, fetchProtocolEndpointsPage, type ProtocolEndpointListItem } from '../api/client'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'

const props = withDefaults(defineProps<{ modelValue: number[]; placeholder?: string }>(), { placeholder: '搜索端点名称或地址' })
const emit = defineEmits<{ 'update:modelValue': [value: number[]] }>()
const search = ref('')
const loading = ref(false)
const error = ref('')
const results = ref<ProtocolEndpointListItem[]>([])
const resultTotal = ref(0)
const bulkAction = ref<'add' | 'remove' | ''>('')
const bulkMessage = ref('')
const known = ref<Record<number, ProtocolEndpointListItem>>({})
const selectedIDs = computed(() => new Set(props.modelValue))
const selectedPage = ref(1)
const selectedPageSize = 50
const selectedPageCount = computed(() => Math.max(1, Math.ceil(props.modelValue.length / selectedPageSize)))
const selectedStart = computed(() => (selectedPage.value - 1) * selectedPageSize)
const selectedEnd = computed(() => Math.min(props.modelValue.length, selectedStart.value + selectedPageSize))
const visibleSelectedIDs = computed(() => props.modelValue.slice(selectedStart.value, selectedEnd.value))
let sequence = 0
let timer: number | undefined
let resultController: AbortController | null = null
let hydrateController: AbortController | null = null
let selectionController: AbortController | null = null

function remember(items: ProtocolEndpointListItem[]) { const next = { ...known.value }; items.forEach(item => { next[item.id] = item }); known.value = next }
function selectedLabel(id: number) { const item = known.value[id]; return item ? `${item.node_name || `节点 #${item.node_id}`} / ${item.name}` : `协议端点 #${id}` }
function formatMultiplier(value: number) { return (Number(value || 1000) / 1000).toLocaleString('zh-CN', { maximumFractionDigits: 3 }) }
function toggle(id: number, selected: boolean) { const next = new Set(props.modelValue); if (selected) next.add(id); else next.delete(id); emit('update:modelValue', [...next]) }
function changeSelectedPage(delta: number) { selectedPage.value = Math.min(selectedPageCount.value, Math.max(1, selectedPage.value + delta)) }

async function loadResults() {
  resultController?.abort(); resultController = new AbortController(); const controller = resultController
  const current = ++sequence; loading.value = true; error.value = ''
  try { const page = await fetchProtocolEndpointsPage({ q: search.value || undefined, active: true, limit: 25 }, { signal: controller.signal }); if (current !== sequence || controller.signal.aborted) return; results.value = page.items; resultTotal.value = page.total; remember(page.items) }
  catch (e: any) { if (current !== sequence || controller.signal.aborted) return; error.value = e?.response?.data?.message || '协议端点搜索失败。'; results.value = []; resultTotal.value = 0 }
  finally { if (current === sequence) loading.value = false }
}
async function applyFilteredSelection(operation: 'add' | 'remove') {
  selectionController?.abort()
  selectionController = new AbortController()
  const controller = selectionController
  const filter = search.value
  bulkAction.value = operation
  bulkMessage.value = ''
  error.value = ''
  try {
    const snapshot = await fetchProtocolEndpointSelection({ q: filter || undefined, active: true }, { signal: controller.signal })
    if (controller.signal.aborted || filter !== search.value) return
    const next = new Set(props.modelValue)
    let changed = 0
    for (const id of snapshot.ids) {
      if (operation === 'add' && !next.has(id)) { next.add(id); changed++ }
      if (operation === 'remove' && next.delete(id)) changed++
    }
    emit('update:modelValue', [...next])
    bulkMessage.value = operation === 'add'
      ? `筛选快照共 ${snapshot.total} 个端点，已加入 ${changed} 个待保存成员。`
      : `筛选快照共 ${snapshot.total} 个端点，已从待保存成员移除 ${changed} 个。`
  } catch (e: any) {
    if (controller.signal.aborted) return
    error.value = e?.response?.data?.error?.fields?.q || e?.response?.data?.message || '协议端点筛选快照解析失败。'
  } finally {
    if (selectionController === controller) bulkAction.value = ''
  }
}
async function hydrateSelected() {
  hydrateController?.abort(); hydrateController = new AbortController(); const controller = hydrateController
  const missing = visibleSelectedIDs.value.filter(id => !known.value[id])
  if (!missing.length) return
  try { const page = await fetchProtocolEndpointsPage({ ids: missing, limit: selectedPageSize }, { signal: controller.signal }); if (controller.signal.aborted) return; remember(page.items) } catch (_) { if (controller.signal.aborted) return /* Numeric fallback labels remain usable. */ }
}
watch(search, () => {
  window.clearTimeout(timer)
  selectionController?.abort()
  bulkAction.value = ''
  bulkMessage.value = ''
  timer = window.setTimeout(loadResults, 250)
})
watch(() => props.modelValue.slice(), () => {
  if (selectedPage.value > selectedPageCount.value) selectedPage.value = selectedPageCount.value
  void hydrateSelected()
})
watch(selectedPage, () => void hydrateSelected())
onMounted(async () => { await Promise.all([loadResults(), hydrateSelected()]) })
onBeforeUnmount(() => { window.clearTimeout(timer); resultController?.abort(); hydrateController?.abort(); selectionController?.abort() })
</script>

<style scoped>
.endpoint-lookup { display: grid; gap: 10px; }.lookup-search { display: flex; align-items: center; gap: 8px; padding: 0 11px; border: 1px solid var(--line-strong); border-radius: 9px; background: var(--surface); color: var(--muted); }.lookup-search:focus-within { border-color: var(--focus-border); box-shadow: 0 0 0 3px var(--primary-soft); }.lookup-search :deep(input) { min-height: 38px; padding-inline: 0; border: 0; box-shadow: none; }.lookup-scope { display: flex; align-items: center; justify-content: space-between; gap: 10px; color: var(--muted); font-size: 10px; }.lookup-scope-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }.lookup-draft-status { display: flex; align-items: flex-start; gap: 6px; margin: 0; padding: 8px 10px; border: 1px solid var(--info-border); border-radius: 8px; color: var(--info-strong); background: var(--info-soft); font-size: 10px; line-height: 1.5; }.lookup-results { min-height: 76px; max-height: 340px; overflow-y: auto; border: 1px solid var(--line); border-radius: 9px; background: var(--surface); }.lookup-results>label { display: grid; grid-template-columns: auto minmax(0,1fr) auto; align-items: center; gap: 9px; padding: 10px 12px; cursor: pointer; }.lookup-results>label+label { border-top: 1px solid var(--line); }.lookup-results>label:hover,.lookup-results>label.selected { background: var(--primary-soft); }.lookup-results strong,.lookup-results small { display: block; }.lookup-results strong { font-size: 11px; }.lookup-results small { margin-top: 2px; color: var(--muted); font-size: 9px; overflow-wrap: anywhere; }.lookup-results em { color: var(--primary); font-size: 10px; font-style: normal; font-weight: 700; }.lookup-empty { margin: 0; padding: 18px; color: var(--muted); text-align: center; font-size: 10px; }.lookup-error { display: flex; align-items: center; gap: 6px; margin: 0; color: var(--danger); font-size: 10px; }.lookup-selected { display: grid; gap: 8px; padding: 11px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface-soft); }.lookup-selected header { display: flex; justify-content: space-between; gap: 10px; }.lookup-selected strong { font-size: 10px; }.lookup-selected small,.lookup-selected>p { margin: 0; color: var(--muted); font-size: 9px; }.selection-chips { display: flex; flex-wrap: wrap; gap: 6px; }.selection-chips>span { display: inline-flex; align-items: center; gap: 5px; padding: 5px 7px; border-radius: 7px; color: var(--text-body); background: var(--surface); font-size: 9px; }.selection-chips button { min-height: 0; padding: 0; border: 0; color: var(--muted); background: transparent; font-size: 14px; line-height: 1; }.selected-pagination { display: flex; align-items: center; justify-content: space-between; gap: 10px; color: var(--muted); font-size: 9px; }.selected-pagination>div { display: flex; gap: 4px; }@media(max-width:640px){.lookup-scope,.lookup-selected header,.selected-pagination{align-items:flex-start;flex-direction:column}.lookup-scope-actions{justify-content:flex-start}}
</style>
