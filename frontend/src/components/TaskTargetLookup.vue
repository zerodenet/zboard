<template>
  <div class="task-target-lookup" role="group" :aria-label="mode === 'users' ? '选择用户' : '选择订阅'">
    <div class="target-search"><UiIcon name="search" /><UiInput v-model.trim="query" :placeholder="mode === 'users' ? '搜索用户邮箱' : '搜索用户邮箱、套餐或 SKU'" :aria-label="mode === 'users' ? '搜索用户' : '搜索订阅'" maxlength="128" /></div>
    <p v-if="error" class="target-error" role="alert"><UiIcon name="alert" />{{ error }}</p>
    <div class="target-results" :aria-busy="loading">
      <label v-for="item in results" :key="item.id" :class="{ selected: selectedIDs.has(item.id) }">
        <UiCheckbox :model-value="selectedIDs.has(item.id)" @update:model-value="toggle(item, Boolean($event))" />
        <span><strong>{{ primaryLabel(item) }}</strong><small>{{ secondaryLabel(item) }}</small></span>
        <StatusBadge :tone="item.status === 'active' ? 'success' : 'neutral'">{{ item.status === 'active' ? '有效' : item.status }}</StatusBadge>
      </label>
      <p v-if="loading" class="target-empty">正在搜索…</p>
      <p v-else-if="!results.length" class="target-empty">{{ query ? '没有匹配的有效目标。' : '当前没有可选的有效目标。' }}</p>
    </div>
    <section class="target-selected">
      <header><strong>已选择 {{ modelValue.length }} 项</strong><small>任务创建后会固化为独立目标并逐项记录进度</small></header>
      <div v-if="modelValue.length" class="target-chips"><span v-for="id in modelValue" :key="id">{{ selectedLabel(id) }}<UiButton variant="ghost" size="sm" icon type="button" :aria-label="`移除 ${selectedLabel(id)}`" @click="remove(id)"><UiIcon name="close" /></UiButton></span></div>
      <p v-else>从上方搜索结果中选择，不需要输入或记忆内部 ID。</p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchSubscriptionsPage, fetchUsersPage, type AdminSubscriptionListItem, type AdminUserListItem } from '../api/client'
import StatusBadge from './StatusBadge.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'

type Target = AdminUserListItem | AdminSubscriptionListItem
const props = defineProps<{ modelValue: number[]; mode: 'users' | 'subscriptions' }>()
const emit = defineEmits<{ 'update:modelValue': [value: number[]] }>()
const query = ref('')
const loading = ref(false)
const error = ref('')
const results = ref<Target[]>([])
const known = ref<Record<number, Target>>({})
const selectedIDs = computed(() => new Set(props.modelValue))
let timer: number | undefined
let controller: AbortController | null = null
let sequence = 0

function isUser(item: Target): item is AdminUserListItem { return 'email' in item }
function primaryLabel(item: Target) { return isUser(item) ? item.email : item.user_email }
function secondaryLabel(item: Target) { return isUser(item) ? `${item.active_subscription_count} 个有效订阅` : `${item.plan_name} / ${item.sku_name}` }
function selectedLabel(id: number) { const item = known.value[id]; return item ? (isUser(item) ? item.email : `${item.user_email} · ${item.plan_name}`) : (props.mode === 'users' ? '已选用户' : '已选订阅') }
function remember(item: Target) { known.value = { ...known.value, [item.id]: item } }
function toggle(item: Target, selected: boolean) { remember(item); const next = new Set(props.modelValue); if (selected) next.add(item.id); else next.delete(item.id); emit('update:modelValue', [...next]) }
function remove(id: number) { emit('update:modelValue', props.modelValue.filter(value => value !== id)) }

async function load() {
  controller?.abort(); controller = new AbortController(); const current = ++sequence
  loading.value = true; error.value = ''
  try {
    const page = props.mode === 'users'
      ? await fetchUsersPage({ q: query.value || undefined, status: 'active', offset: 0, limit: 20 }, { signal: controller.signal })
      : await fetchSubscriptionsPage({ q: query.value || undefined, status: 'active', offset: 0, limit: 20 }, { signal: controller.signal })
    if (current !== sequence || controller.signal.aborted) return
    results.value = page.items
    for (const item of page.items) remember(item)
  } catch (cause: any) {
    if (current !== sequence || controller?.signal.aborted) return
    error.value = cause?.response?.data?.message || '目标搜索失败。'
    results.value = []
  } finally { if (current === sequence) loading.value = false }
}

watch(query, () => { window.clearTimeout(timer); timer = window.setTimeout(load, 250) })
watch(() => props.mode, () => { query.value = ''; results.value = []; void load() })
onMounted(load)
onBeforeUnmount(() => { window.clearTimeout(timer); controller?.abort() })
</script>

<style scoped>
.task-target-lookup{display:grid;gap:10px}.target-search{display:flex;align-items:center;gap:8px;padding:0 11px;border:1px solid var(--line-strong);border-radius:9px;background:var(--surface);color:var(--muted)}.target-search:focus-within{border-color:var(--focus-border);box-shadow:0 0 0 3px var(--primary-soft)}.target-search :deep(input){min-height:38px;padding-inline:0;border:0;box-shadow:none}.target-error{display:flex;align-items:center;gap:6px;margin:0;color:var(--danger);font-size:10px}.target-results{min-height:78px;max-height:300px;overflow-y:auto;border:1px solid var(--line);border-radius:9px;background:var(--surface)}.target-results>label{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:9px;padding:10px 12px;cursor:pointer}.target-results>label+label{border-top:1px solid var(--line)}.target-results>label:hover,.target-results>label.selected{background:var(--primary-soft)}.target-results strong,.target-results small{display:block}.target-results strong{font-size:11px}.target-results small{margin-top:2px;color:var(--muted);font-size:9px}.target-empty{margin:0;padding:20px;color:var(--muted);text-align:center;font-size:10px}.target-selected{display:grid;gap:8px;padding:11px;border:1px solid var(--line);border-radius:9px;background:var(--surface-soft)}.target-selected header{display:flex;justify-content:space-between;gap:12px}.target-selected strong{font-size:10px}.target-selected small,.target-selected>p{margin:0;color:var(--muted);font-size:9px}.target-chips{display:flex;flex-wrap:wrap;gap:6px}.target-chips>span{display:inline-flex;align-items:center;gap:5px;padding:5px 7px;border-radius:7px;background:var(--surface);font-size:9px}.target-chips button{min-height:0;padding:0;border:0;background:transparent}@media(max-width:640px){.target-selected header{align-items:flex-start;flex-direction:column}}
</style>
