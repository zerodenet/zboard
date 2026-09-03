<template>
  <WorkbenchFilterChip label="订阅" :active="Boolean(model)" :value-label="selectedLabel" wide @open="open" @clear="choose('')">
    <template #default="{ close }">
      <div class="workbench-filter-form">
        <UiInput v-model="draft" aria-label="搜索订阅" placeholder="套餐、规格名称或订阅编号" @keyup.enter="search" />
        <UiButton size="sm" @click="search">搜索</UiButton>
      </div>
      <PageAlert v-if="targetError" tone="danger" title="当前订阅读取失败">{{ targetError }} <UiButton @click="target.load()">重试当前订阅</UiButton></PageAlert>
      <PageAlert v-if="error" tone="danger" title="订阅选项加载失败">{{ error }} <UiButton @click="table.load()">重试订阅选项</UiButton></PageAlert>
      <p v-if="loading" role="status">正在加载订阅选项…</p>
      <div v-else-if="!error" class="workbench-filter-options" role="listbox" aria-label="订阅选项">
        <UiButton variant="ghost" role="option" :aria-selected="!model" @click="choose('', close)">全部订阅</UiButton>
        <UiButton v-for="item in items" :key="item.id" variant="ghost" role="option" :aria-selected="String(item.id) === model" @click="choose(String(item.id), close)">{{ label(item) }}</UiButton>
        <p v-if="loaded && !items.length">没有匹配的订阅</p>
      </div>
      <TablePager v-if="loaded && !error" :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" />
    </template>
  </WorkbenchFilterChip>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { fetchAccountSubscriptionsPage, type AdminSubscriptionListItem } from '../../api/client'
import PageAlert from '../../components/PageAlert.vue'
import TablePager from '../../components/TablePager.vue'
import UiInput from '../../components/UiInput.vue'
import UiButton from '../../components/UiButton.vue'
import WorkbenchFilterChip from '../../components/WorkbenchFilterChip.vue'
import { useRemoteTable } from '../../composables/useRemoteTable'
import { useRemoteResource } from '../../composables/useRemoteResource'
import { optionalQueryID } from '../../api/queryID'

const model = defineModel<string>({ default: '' })
const emit = defineEmits<{ apply: [] }>()
const offset = ref(0), limit = ref(25), draft = ref(''), query = ref('')
const table = useRemoteTable<AdminSubscriptionListItem>({
  offset, limit,
  fetchPage: ({ signal }) => fetchAccountSubscriptionsPage({ q: query.value, offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: '订阅选项加载失败。',
})
const { items, total, loading, hasLoaded: loaded, error } = table
const target = useRemoteResource<AdminSubscriptionListItem | null>({
  initial: () => null,
  fetch: async ({ signal }) => {
    const id = optionalQueryID(model.value, '订阅')
    if (id === undefined) throw new Error('invalid subscription id')
    const page = await fetchAccountSubscriptionsPage({ subscriptionId: id, limit: 1 }, { signal })
    return page.items.find(item => item.id === id) || null
  },
  errorMessage: '当前订阅无法读取，请重试。',
})
const { error: targetError } = target
function label(item: AdminSubscriptionListItem) { return [item.plan_name, item.sku_name, `#${item.id}`].filter(Boolean).join(' · ') }
const selectedLabel = computed(() => {
  if (!model.value) return ''
  if (target.data.value) return label(target.data.value)
  const state = target.error.value ? '读取失败' : target.loaded.value ? '不存在或不可访问' : '加载中'
  return `#${model.value}（${state}）`
})
watch(model, value => {
  target.reset()
  if (!value) return
  const item = items.value.find(item => String(item.id) === value)
  if (item) target.replace(item)
  else void target.load()
}, { immediate: true, flush: 'sync' })
function open() { if (!loaded.value && !loading.value) void table.load() }
async function search() { query.value = draft.value.trim(); offset.value = 0; table.reset(); await table.load() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; table.reset(); await table.load() }
async function choose(value: string, close?: (restoreFocus?: boolean) => void) { model.value = value; await nextTick(); emit('apply'); close?.(true) }
</script>
