<template>
  <div class="entry-members">
    <UiInput v-model="search" aria-label="搜索前置线路" placeholder="搜索入口或落地协议名称" />
    <p v-if="loading">正在加载前置线路…</p>
    <p v-else-if="error" role="alert">{{ error }} <UiButton size="sm" variant="secondary" @click="load">重试</UiButton></p>
    <p v-else-if="!entries.length">暂无前置线路。请先在「网络前置」创建入口与落地组合。</p>
    <div v-else class="entry-options">
      <label v-for="entry in filtered" :key="entry.id" class="entry-option">
        <UiCheckbox :model-value="modelValue.includes(entry.id)" :disabled="!entry.enabled && !modelValue.includes(entry.id)" @update:model-value="toggle(entry.id, Boolean($event))" />
        <span><strong>{{ entry.name }} · {{ entry.endpoint_name }}</strong><small>{{ entry.node_name }} → {{ entry.endpoint_name }} · {{ entry.enabled ? '可分配' : '已停用' }}{{ entry.pending ? ' · 等待发布' : '' }}</small></span>
      </label>
    </div>
    <div v-for="id in missing" :key="id" class="entry-option"><span>已选前置线路 #{{ id }}（当前列表不可用）</span><UiButton size="sm" variant="ghost" @click="toggle(id, false)">移除</UiButton></div>
    <small>已选 {{ modelValue.length }} 条。前置线路与 B 直连分别授权，不会自动互相包含。</small>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchNetworkEntries, type NetworkEntry } from '../api/client'
import UiInput from './UiInput.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiButton from './UiButton.vue'
const props = defineProps<{ modelValue: number[] }>()
const emit = defineEmits<{ 'update:modelValue': [number[]] }>()
const entries = ref<NetworkEntry[]>([]), search = ref(''), loading = ref(false), error = ref('')
const filtered = computed(() => entries.value.filter(entry => `${entry.name} ${entry.endpoint_name} ${entry.node_name}`.toLowerCase().includes(search.value.trim().toLowerCase())))
const missing = computed(() => props.modelValue.filter(id => !entries.value.some(entry => entry.id === id)))
function toggle(id: number, selected: boolean) { emit('update:modelValue', selected ? [...new Set([...props.modelValue, id])] : props.modelValue.filter(value => value !== id)) }
async function load() {
  loading.value = true; error.value = ''
  try { entries.value = await fetchNetworkEntries() }
  catch { error.value = '前置线路加载失败，已有选择已保留。' }
  finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.entry-members{display:grid;gap:10px;margin-top:8px}.entry-options{display:grid;gap:6px;max-height:320px;overflow:auto}.entry-option{display:flex;align-items:center;gap:10px;padding:10px;border:1px solid var(--line);border-radius:8px;background:var(--surface)}.entry-option span{display:grid;gap:4px;min-width:0}.entry-option strong{font-size:12px;overflow-wrap:anywhere}small{color:var(--muted);font-size:11px}
</style>
