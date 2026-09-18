<template>
  <section class="stack" aria-label="邮件执行历史">
    <p>这里保留每次尝试的原始渠道结果。渠道接收不代表已到收件箱；后续人工核验不会覆盖这些记录。</p>
    <PageAlert v-if="error" tone="danger" title="执行历史加载失败">{{ error }}</PageAlert>
    <UiButton variant="secondary" :loading="loading" @click="load">刷新</UiButton>
    <DataTable v-if="page?.items.length" caption="邮件逐次执行记录" :row-count="page.total" :min-width="560">
      <thead><tr><th>尝试次数</th><th>渠道结果</th><th>开始时间</th><th>结果记录时间</th></tr></thead>
      <tbody><tr v-for="row in page.items" :key="row.id"><td>{{ row.attempt }}</td><td>{{ labels[row.acceptance] || '结果未知' }}</td><td><TimeBadge :value="row.started_at" /></td><td><TimeBadge :value="row.finished_at" /></td></tr></tbody>
    </DataTable>
    <p v-else-if="!loading && !error">暂无逐次执行记录，旧任务不会补造历史。</p>
    <TablePager v-if="page" :total="page.total" :offset="offset" :limit="25" :page-sizes="[25]" :loading="loading" @change="changePage" />
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { fetchMailDeliveryHistory } from '../api/client'
import DataTable from './DataTable.vue'
import PageAlert from './PageAlert.vue'
import TablePager from './TablePager.vue'
import TimeBadge from './TimeBadge.vue'
const props = defineProps<{ taskID: number; itemID: number }>()
const labels = { accepted: '渠道已接收', not_accepted: '渠道未接收', unknown: '接收结果未知' }
const page = ref<Awaited<ReturnType<typeof fetchMailDeliveryHistory>> | null>(null)
const offset = ref(0), loading = ref(false), error = ref('')
let request: AbortController | undefined
async function load() {
 request?.abort(); const current = new AbortController(); request = current
 loading.value = true; error.value = ''
 try { const result = await fetchMailDeliveryHistory(props.taskID, props.itemID, offset.value, current.signal); if (request === current && !current.signal.aborted) page.value = result }
 catch { if (request === current && !current.signal.aborted) { page.value = null; error.value = '请稍后重试。' } }
 finally { if (request === current) loading.value = false }
}
function changePage(value: { offset: number }) { offset.value = value.offset; void load() }
watch(() => [props.taskID, props.itemID], () => { page.value = null; offset.value = 0; void load() }, { immediate: true })
onBeforeUnmount(() => request?.abort())
</script>
