<template>
  <div>
    <UiAutocomplete :model-value="input" :suggestions="items" option-label="label" :input-id="inputId" :aria-label="label" :placeholder="placeholder" :disabled="disabled" :loading="loading" force-selection dropdown @update:model-value="updateInput" @open="openList" @complete="search" @item-select="select" @clear="clear">
      <template #option="{ option }"><span>{{ option.label }}</span></template>
      <template #footer>
        <div v-if="total > 25" class="lookup-pager">
          <UiButton size="sm" variant="ghost" :disabled="loading || offset === 0" @click.stop="page(-25)">上一页</UiButton>
          <small>{{ Math.floor(offset / 25) + 1 }} / {{ Math.ceil(total / 25) }}</small>
          <UiButton size="sm" variant="ghost" :disabled="loading || offset + 25 >= total" @click.stop="page(25)">下一页</UiButton>
        </div>
      </template>
    </UiAutocomplete>
    <small v-if="error" role="alert">{{ error }} <UiButton size="sm" variant="ghost" @click="load">重试</UiButton></small>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import UiAutocomplete from './UiAutocomplete.vue'
import UiButton from './UiButton.vue'
import type { AssignmentChoice } from '../utils/adminOrderAssignment'
const props = defineProps<{ modelValue: AssignmentChoice | null; label: string; inputId?: string; placeholder?: string; disabled?: boolean; fetchPage: (query: string, offset: number, signal: AbortSignal) => Promise<{ items: AssignmentChoice[]; total: number }> }>()
const emit = defineEmits<{ 'update:modelValue': [value: AssignmentChoice | null] }>()
const input = ref<AssignmentChoice | string | null>(null)
const items = ref<AssignmentChoice[]>([]), total = ref(0), offset = ref(0), query = ref(''), loading = ref(false), error = ref('')
let controller: AbortController | null = null
let sequence = 0
watch(() => props.modelValue, value => { if (value || typeof input.value !== 'string') input.value = value }, { immediate: true })
function updateInput(value: AssignmentChoice | string | null) { input.value = value; if (typeof value === 'string' || !value) emit('update:modelValue', null) }
function select(event: { value: AssignmentChoice }) { input.value = event.value; emit('update:modelValue', event.value) }
function clear() { input.value = null; emit('update:modelValue', null); search({ query: '' }) }
// A selected label is display text, not a search filter. Opening an untouched
// or selected field must browse its scoped default list without a keyword.
function openList() { search({ query: typeof input.value === 'string' ? input.value : '' }) }
function search(event: { query: string }) { query.value = event.query.trim(); offset.value = 0; void load() }
function page(delta: number) {
  const next = offset.value + delta
  if (loading.value || props.disabled || next < 0 || next >= total.value) return
  offset.value = next; void load()
}
async function load() {
  if (props.disabled) return
  controller?.abort(); controller = new AbortController(); const current = controller, revision = ++sequence
  loading.value = true; error.value = ''; items.value = []
  try {
    const result = await props.fetchPage(query.value, offset.value, current.signal)
    if (revision !== sequence) return
    items.value = result.items; total.value = result.total
  } catch {
    if (revision === sequence && !current.signal.aborted) { items.value = []; total.value = 0; error.value = `${props.label}加载失败，请重试。` }
  } finally { if (revision === sequence) loading.value = false }
}
watch(() => props.disabled, disabled => {
  if (!disabled) return
  sequence++; controller?.abort(); loading.value = false; items.value = []; total.value = 0; error.value = ''
})
onBeforeUnmount(() => { sequence++; controller?.abort() })
</script>
<style scoped>
.lookup-pager { display: flex; align-items: center; justify-content: space-between; padding: 8px; }
</style>
