<template>
  <section class="standard-page runtime-jobs" :class="{ 'queue-focused': queueFocused }">
    <PageHeader :title="queueFocused ? `${queueName}队列` : '后台任务'" eyebrow="Operations">
      <template #actions><RouterLink class="task-link" :to="queueFocused ? '/admin/runtime-jobs' : '/admin/tasks'">{{ queueFocused ? '全部后台任务' : '运营任务' }}</RouterLink><PageRefreshButton label="刷新后台任务" :loading="loading || (queueFocused && queueLoading)" @click="refresh" /></template>
    </PageHeader>
    <div v-if="error && !queueFocused" class="runtime-error" role="alert"><strong>运行状态未更新</strong><span>{{ error }}</span><UiButton variant="ghost" size="sm" :loading="loading" @click="refresh">重试</UiButton></div>
    <section v-if="!queueFocused" class="registered-section" aria-label="已注册任务">
      <div class="runtime-heading"><h2>已注册任务</h2><span v-if="registeredJobs">本实例 · {{ registeredJobs.length }} 个</span></div>
      <p v-if="registryLoading && !registeredJobs" role="status">正在读取任务名录…</p>
      <p v-else-if="registryError" class="registry-unavailable">{{ registryError }} <UiButton variant="ghost" size="sm" @click="refreshRegisteredJobs">重试</UiButton></p>
      <p v-else-if="registeredJobs && !registeredJobs.length" class="registry-unavailable">当前进程没有已注册任务。</p>
      <div v-if="registeredJobs?.length" class="registered-list">
        <div v-for="job in registeredJobs" :key="job.id" class="registered-row">
          <div class="registered-name"><strong>{{ job.name }}</strong><small :title="job.id">{{ registeredOwner(job.owner) }}</small></div>
          <span class="registered-cadence">{{ registeredCadence(job.interval_seconds) }}</span>
          <StatusBadge v-if="registeredStatus(job.id)" :tone="tone(registeredStatus(job.id)!.state)">{{ state(registeredStatus(job.id)!.state) }}</StatusBadge>
          <span v-else class="registered-unknown">状态未取得</span>
          <span v-if="registeredStatus(job.id)?.last_finished_at" class="registered-last">最近 <TimeBadge :value="registeredStatus(job.id)!.last_finished_at" /></span>
        </div>
      </div>
    </section>
    <p v-if="!data && loading && !queueFocused" role="status">正在读取运行状态…</p>
    <template v-if="data || queueFocused">
      <template v-if="data && !queueFocused">
      <div class="runtime-meta"><span>更新 <TimeBadge :value="data.as_of" /></span><div v-if="data.issues?.length" class="runtime-issue"><strong>{{ data.issues.length }} 项状态不可用</strong><details><summary>原因</summary><p v-for="issue in data.issues" :key="issue.section">{{ issueText(issue) }}</p></details><UiButton variant="ghost" size="sm" :loading="loading" @click="refresh">重试</UiButton></div></div>
      <section v-if="data.execution_queue" class="execution-strip" aria-label="全局执行队列"><strong>执行队列</strong><span>等待 {{ data.execution_queue.pending }}</span><span>执行中 {{ data.execution_queue.running }}</span><span>延后 {{ data.execution_queue.delayed }}</span><span v-if="data.execution_queue.unknown">待核验 {{ data.execution_queue.unknown }}</span><span v-if="data.execution_queue.maintenance_reserved">维护中</span></section>
      <div class="runtime-heading"><h2>待处理队列</h2></div>
      <section class="queue-grid" aria-label="业务待处理概览">
        <button v-for="queue in data.queues" :key="queue.id" class="queue-card" :class="{ selected: selected === queue.id }" type="button" :aria-pressed="selected === queue.id" @click="selectQueue(queue.id)">
          <span class="queue-title">{{ queue.name }}<span>→</span></span>
          <span class="queue-counts"><span><strong>{{ queue.pending }}</strong>{{ queue.id === 'registration_messages' ? '待处理' : '等待' }}</span><span v-if="queue.running"><strong>{{ queue.running }}</strong>执行中</span><span v-if="queue.delayed"><strong>{{ queue.delayed }}</strong>延后</span><span v-if="queue.failed || queue.stale"><strong>{{ queue.failed + queue.stale }}</strong>异常</span></span>
        </button>
        <div class="queue-card spool-card"><span class="queue-title">流量事件入账</span><span v-if="data.runtime.event_spool" class="queue-counts"><span><strong>{{ data.runtime.event_spool.pending_events }}</strong>待入账</span><span><strong>{{ bytes(data.runtime.event_spool.pending_bytes) }}</strong>数据量</span></span><span v-else class="queue-foot">{{ sectionIssue('runtime') ? '状态未取得' : '未启用' }}</span></div>
      </section>
      <details class="runtime-diagnostics"><summary>执行明细与插件状态</summary>
      <section class="runtime-section">
        <div class="runtime-heading"><h2>任务执行情况</h2><span>共享执行名额 {{ data.execution_concurrency || 4 }} 个 · 外部操作最多占用 {{ data.external_execution_concurrency || 3 }} 个</span></div>
        <DataTable caption="后台任务执行情况" :row-count="data.jobs.length" :min-width="1000">
          <thead><tr><th>任务</th><th>执行周期</th><th>运行状态</th><th>最近结果</th><th>最近完成 / 耗时</th><th>下次扫描</th><th>执行 / 失败</th></tr></thead>
          <tbody><tr v-for="job in data.jobs" :key="job.id"><td><strong>{{ job.name }}</strong><small v-if="job.last_error" class="job-error">{{ job.last_error }}</small></td><td>{{ interval(job) }}<small v-if="job.timezone">{{ job.timezone }} · {{ job.misfire_policy === 'skip' ? '错过跳过' : '错过合并一次' }}</small></td><td><StatusBadge :tone="tone(job.state)">{{ state(job.state) }}<template v-if="job.running"> · {{ job.running }}</template></StatusBadge></td><td><StatusBadge :tone="tone(job.last_result)">{{ state(job.last_result) }}</StatusBadge></td><td><TimeBadge :value="job.last_finished_at" /><small v-if="job.last_finished_at">{{ duration(job.last_duration_ms) }}</small></td><td><TimeBadge v-if="job.next_scan_at" :value="job.next_scan_at" /><span v-else>{{ job.state === 'running' ? '已保留下次计划' : '等待事件或扫描' }}</span></td><td>{{ job.runs }} / {{ job.failures }}<small v-if="job.missed_runs">跳过 {{ job.missed_runs }}</small></td></tr></tbody>
        </DataTable>
        <p v-if="sectionIssue('execution')" class="detail-note">执行统计未取得。</p>
        <p v-else-if="!data.jobs.length">暂无执行记录。</p>
      </section>
      <section class="runtime-section" aria-label="插件任务">
        <div class="runtime-heading"><h2>插件任务</h2><span>来自插件注册 · 每插件串行，与内置任务共用 {{ data.execution_concurrency || data.plugin_task_concurrency || 4 }} 个执行名额</span></div>
        <p v-if="sectionIssue('plugin_tasks')" class="detail-note">插件任务状态暂不可用；插件宿主租约仍按已取得的状态展示。</p>
        <p v-else-if="!data.plugin_host" class="detail-note">插件宿主不可用，暂时无法读取注册任务。</p>
        <template v-else-if="pluginTasks.length">
          <div class="plugin-queue-summary" aria-label="插件任务队列状态"><span>已注册 <strong>{{ pluginTasks.length }}</strong></span><span>到期等待 <strong>{{ pluginTasks.filter(task => task.state === 'queued').length }}</strong></span><span>执行中 <strong>{{ pluginTasks.reduce((sum, task) => sum + task.running, 0) }}</strong></span><span>最近失败或中断 <strong>{{ pluginTasks.filter(task => ['failed', 'interrupted', 'unknown'].includes(task.last_result)).length }}</strong></span></div>
          <DataTable caption="插件注册任务" :row-count="pluginTasks.length" :min-width="1000">
            <thead><tr><th>任务 / 所属插件</th><th>执行周期 / 超时</th><th>运行状态</th><th>最近结果</th><th>最近完成 / 耗时</th><th>下次计划</th><th>执行 / 失败</th></tr></thead>
            <tbody><tr v-for="task in pluginTaskPage" :key="task.id"><td><strong>{{ task.name }}</strong><small><RouterLink :to="`/admin/plugins/${task.plugin_id}`">{{ task.plugin_name }} · {{ task.plugin_id }}</RouterLink></small><small v-if="task.last_error" class="job-error">{{ task.last_error }}</small></td><td>{{ interval(task) }}<small>超时 {{ task.timeout_seconds }} 秒</small></td><td><StatusBadge :tone="tone(task.state)">{{ state(task.state) }}</StatusBadge></td><td><StatusBadge :tone="tone(task.last_result)">{{ state(task.last_result) }}</StatusBadge></td><td><TimeBadge :value="task.last_finished_at" /><small v-if="task.last_finished_at">{{ duration(task.last_duration_ms) }}</small></td><td><TimeBadge v-if="task.next_scan_at" :value="task.next_scan_at" /><span v-else>{{ task.running ? '本轮结束后安排' : '等待启用或调度' }}</span></td><td>{{ task.runs }} / {{ task.failures }}</td></tr></tbody>
          </DataTable>
          <TablePager :total="pluginTasks.length" :offset="pluginOffset" :limit="25" :page-sizes="[25]" @change="value => pluginOffset = value.offset" />
        </template>
        <p v-else class="detail-note">暂无插件任务。</p>
      </section>
      </details>
      </template>
      <section v-if="selected && (queueFocused || data?.queues?.length)" class="runtime-section" aria-label="队列明细">
        <div v-if="!queueFocused" class="runtime-heading"><h2>{{ queueName }}队列</h2></div>
        <div v-else-if="queuePage" class="focused-summary"><span>{{ queuePage.total }} 条队列记录</span><span>每 15 秒刷新</span></div>
        <PageAlert v-if="queueError || retryError" tone="danger" title="队列操作未完成">{{ retryError || queueError }}<template #actions><UiButton variant="secondary" size="sm" :loading="queueLoading" @click="reloadQueue">重新读取队列</UiButton></template></PageAlert>
        <DataTable v-if="queuePage?.items.length" caption="队列明细" :row-count="queuePage.total" :min-width="800">
          <thead><tr><th class="table-primary-column">任务 / 目标</th><th>状态</th><th v-if="selected !== 'registration_messages'" data-column-priority="2">尝试</th><th>{{ selected === 'registration_messages' ? '事件时间' : '下次领取' }}</th><th v-if="selected !== 'registration_messages'" data-column-priority="3">租约到期</th><th v-if="selected === 'node_publish'" class="table-action-column">操作</th></tr></thead>
          <tbody><tr v-for="item in queuePage.items" :key="item.id"><td class="table-primary-column"><div class="queue-task"><span v-if="selected === 'registration_messages'">账户 #{{ item.id }} · 注册事件</span><RouterLink v-else :to="selected === 'admin_tasks' ? `/admin/tasks?task=${item.id}` : `/admin/nodes?node=${item.id}`">{{ selected === 'admin_tasks' ? '任务' : '节点' }} #{{ item.id }}</RouterLink><button v-if="item.last_error" type="button" class="queue-error-trigger" @click="queueFailure = { title: `${queueNames[selected] || '任务'} #${item.id}`, detail: item.last_error }">查看错误</button></div></td><td><StatusBadge :tone="tone(item.state)">{{ selected === 'registration_messages' ? '待处理' : state(item.state) }}</StatusBadge></td><td v-if="selected !== 'registration_messages'" data-column-priority="2">{{ item.attempts }}</td><td><TimeBadge :value="selected === 'registration_messages' ? item.created_at : item.next_attempt_at" /></td><td v-if="selected !== 'registration_messages'" data-column-priority="3"><TimeBadge :value="item.lease_until" /></td><td v-if="selected === 'node_publish'" class="table-action-column"><UiButton v-if="['delayed', 'stale'].includes(item.state)" size="sm" variant="secondary" :loading="retryingID === item.id" @click="retryPublication(item.id)">立即重试</UiButton></td></tr></tbody>
        </DataTable>
        <p v-else-if="queuePage && !queueLoading && !queueError">此队列暂无待处理任务。</p><p v-if="queueLoading" role="status">正在读取队列…</p>
        <TablePager v-if="queuePage" :total="queuePage.total" :offset="offset" :limit="25" :page-sizes="[25]" :loading="queueLoading" @change="changePage" />
      </section>
      <template v-if="data && !queueFocused">
      <details class="runtime-diagnostics" @toggle="setHistoryOpen"><summary>执行历史与插件通讯</summary>
      <RuntimeJobHistory v-if="historyOpen" :as-of="data.as_of" :names="Object.fromEntries([...data.jobs, ...pluginTasks].map(job => [job.id, job.name]))" @resolved="refresh" />
      <section class="runtime-section plugin-status"><div><h2>插件通讯</h2><p>宿主每 10 秒续约与检查；短暂数据库故障会在有效租约内重试。</p></div><template v-if="data.plugin_host"><StatusBadge :tone="data.plugin_host.state === 'active' ? 'success' : 'warning'">{{ data.plugin_host.state === 'active' ? '持有运行租约' : '等待接管 / 恢复' }}</StatusBadge><span>租约到期 <TimeBadge :value="data.plugin_host.lease_until" /></span><span>累计续约失败 {{ data.plugin_host.renewal_failures }}</span></template><span v-else>插件宿主不可用</span><RouterLink to="/admin/plugins">查看各插件状态 →</RouterLink><p class="detail-note">宿主租约表示执行权有效，具体插件的配置、进程及业务连通性请在插件管理中检查。</p></section>
      </details>
      </template>
    </template>
    <ModalDialog :open="Boolean(queueFailure)" :title="queueFailure?.title || '队列错误'" @close="queueFailure = null"><OutputBlock v-if="queueFailure" :value="queueFailure.detail" label="最近错误" tone="danger" /></ModalDialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { fetchRegisteredJobs, fetchRuntimeJobs, fetchRuntimeQueue, retryNodePublication, type RuntimeJobs, type RuntimeJob, type QueuePage, type RegisteredJob } from '../api/runtimeJobs'
import RuntimeJobHistory from '../components/RuntimeJobHistory.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import PageAlert from '../components/PageAlert.vue'
import UiButton from '../components/UiButton.vue'
import DataTable from '../components/DataTable.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TablePager from '../components/TablePager.vue'
import ModalDialog from '../components/ModalDialog.vue'
import OutputBlock from '../components/OutputBlock.vue'
const data = ref<RuntimeJobs | null>(null), queuePage = ref<QueuePage | null>(null)
const loading = ref(false), queueLoading = ref(false), error = ref(''), queueError = ref(''), retryError = ref(''), retryingID = ref(0)
const registeredJobs = ref<RegisteredJob[] | null>(null), registryLoading = ref(false), registryError = ref('')
const queueFailure = ref<{ title: string; detail: string } | null>(null)
const route = useRoute()
const router = useRouter()
const queueNames: Record<string, string> = { admin_tasks: '运营任务', node_publish: '节点配置发布', registration_messages: '注册消息' }
function isKnownQueue(queue: unknown): queue is string { return typeof queue === 'string' && Object.prototype.hasOwnProperty.call(queueNames, queue) }
const requestedQueue = isKnownQueue(route.query.queue) ? route.query.queue : 'admin_tasks'
const selected = ref(requestedQueue), offset = ref(0), pluginOffset = ref(0)
const queueFocused = computed(() => isKnownQueue(route.query.queue))
const selectedQueue = computed(() => data.value?.queues?.find(queue => queue.id === selected.value))
const queueName = computed(() => selectedQueue.value?.name || queueNames[selected.value] || '任务')
const pluginTasks = computed(() => data.value?.plugin_tasks || [])
const statusByID = computed(() => new Map([...(data.value?.jobs || []), ...pluginTasks.value].map(job => [job.id, job])))
const historyOpen = ref(false)
const pluginTaskPage = computed(() => pluginTasks.value.slice(Math.min(pluginOffset.value, Math.max(0, Math.ceil(pluginTasks.value.length / 25) - 1) * 25), pluginOffset.value + 25))
let controller: AbortController | undefined, queueController: AbortController | undefined, registryController: AbortController | undefined, timer: ReturnType<typeof setInterval> | undefined
const labels: Record<string, string> = { idle: '等待下轮', running: '执行中', paused: '维护暂停', stopped: '已停止', succeeded: '成功', failed: '失败', never: '尚未执行', pending: '等待执行', delayed: '延后执行', stale: '租约失效', draft: '未入队', queued: '到期等待', unknown: '结果待核验', yielded: '继续处理', disabled: '已禁用', unavailable: '不可执行', interrupted: '已中断', canceled: '已取消' }
function state(value: string) { return labels[value] || value }
function tone(value: string): 'neutral' | 'success' | 'danger' | 'warning' | 'info' { return value === 'succeeded' ? 'success' : value === 'failed' ? 'danger' : ['unknown', 'stale', 'paused', 'stopped', 'interrupted', 'unavailable'].includes(value) ? 'warning' : value === 'running' ? 'info' : 'neutral' }
function duration(ms: number) { return ms < 1000 ? `${ms} 毫秒` : `${(ms / 1000).toFixed(1)} 秒` }
function interval(job: RuntimeJob) { const n = job.interval_seconds; const value = n >= 3600 ? `${n / 3600} 小时` : n >= 60 ? `${n / 60} 分钟` : `${n} 秒`; return n <= 0 ? (job.kind === 'on_demand' ? '按需执行' : '事件驱动') : job.kind === 'queue' ? `唤醒 / ${value}轮询` : `每 ${value}` }
function registeredCadence(seconds: number) { return seconds <= 0 ? '按需' : seconds >= 3600 ? `每 ${seconds / 3600} 小时` : seconds >= 60 ? `每 ${seconds / 60} 分钟` : `每 ${seconds} 秒` }
function registeredOwner(owner: string) { return owner === 'system' ? '内置' : pluginTasks.value.find(task => `plugin:${task.plugin_id}` === owner)?.plugin_name || owner.replace(/^plugin:/, '插件 · ') }
function registeredStatus(id: string) { if (error.value || sectionIssue(id.startsWith('plugin:') ? 'plugin_tasks' : 'execution')) return undefined; return statusByID.value.get(id) }
function setHistoryOpen(event: Event) { historyOpen.value = (event.target as HTMLDetailsElement).open }
function bytes(n: number) { return n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB` }
function sectionIssue(section: string) { return data.value?.issues?.some(issue => issue.section === section) || false }
function issueText(issue: { section: string; message: string }) { const ms = data.value?.read_duration_ms?.[issue.section]; return typeof ms === 'number' && Number.isFinite(ms) ? `${issue.message}（读取 ${ms} 毫秒）` : issue.message }
function requestError(cause: any, fallback: string) { return cause?.response?.data?.message || fallback }
async function refreshRegisteredJobs() {
  if (registryLoading.value) return
  const current = new AbortController(); registryController = current; registryLoading.value = true
  try { const result = await fetchRegisteredJobs(current.signal); if (!current.signal.aborted) { registeredJobs.value = result; registryError.value = '' } }
  catch (cause: any) { if (!current.signal.aborted) { registeredJobs.value = null; registryError.value = cause?.response?.status === 404 ? '当前后端尚未提供任务名录接口。' : requestError(cause, '任务名录读取失败。') } }
  finally { if (registryController === current) registryLoading.value = false }
}
async function refreshQueue() {
  if (!selected.value) { queuePage.value = null; queueError.value = ''; return }
  queueController?.abort(); const current = new AbortController(); queueController = current; queueLoading.value = true
  try { const result = await fetchRuntimeQueue(selected.value, offset.value, current.signal); if (!current.signal.aborted) { queuePage.value = result; queueError.value = '' } }
  catch (cause: any) { if (!current.signal.aborted) { queuePage.value = null; queueError.value = requestError(cause, '读取失败，请稍后重试。') } }
  finally { if (queueController === current) queueLoading.value = false }
}
async function refresh() {
  if (queueFocused.value) { await refreshQueue(); return }
  if (loading.value) return
  void refreshRegisteredJobs()
  loading.value = true; const current = new AbortController(); controller = current
  let loaded = false
  try {
    const result = await fetchRuntimeJobs(current.signal)
    if (!current.signal.aborted) {
      result.jobs ||= []
      result.queues ||= []
      data.value = result; error.value = ''; loaded = true
      if (!isKnownQueue(selected.value) && !result.queues.some(queue => queue.id === selected.value)) {
        selected.value = result.queues[0]?.id || ''; offset.value = 0; queuePage.value = null; queueError.value = ''
      }
    }
  }
  catch (cause: any) { if (!current.signal.aborted) error.value = requestError(cause, '暂时无法读取后台执行情况。') }
  finally { if (!current.signal.aborted) loading.value = false }
  if (!current.signal.aborted && loaded && !queueFocused.value) await refreshQueue()
}
function selectQueue(id: string) { if (id === selected.value) return; selected.value = id; offset.value = 0; queuePage.value = null; queueFailure.value = null; queueError.value = ''; void router.replace({ query: { ...route.query, queue: id } }); void refreshQueue() }
watch(() => route.query.queue, (queue, previousQueue) => {
  if (isKnownQueue(queue) && queue !== selected.value) selectQueue(queue)
  else if (isKnownQueue(queue) && !isKnownQueue(previousQueue) && !queueLoading.value && !queuePage.value) void refreshQueue()
  else if (!isKnownQueue(queue) && isKnownQueue(previousQueue)) {
    selected.value = 'admin_tasks'; offset.value = 0; queuePage.value = null; queueError.value = ''; void refresh()
  }
})
function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; void refreshQueue() }
function reloadQueue() { retryError.value = ''; void refreshQueue() }
async function retryPublication(id: number) {
  if (retryingID.value) return
  retryError.value = ''; retryingID.value = id
  try { await retryNodePublication(id); await refresh() }
  catch (cause: any) { retryError.value = requestError(cause, '重试请求未保存，请刷新队列后再试。') }
  finally { retryingID.value = 0 }
}
onMounted(() => { void refresh(); timer = setInterval(() => { if (!document.hidden && !queueLoading.value) void refresh() }, 15000) })
onBeforeUnmount(() => { clearInterval(timer); controller?.abort(); queueController?.abort(); registryController?.abort() })
</script>
<style scoped>
.global-queue .queue-counts{flex-wrap:wrap;gap:1.5rem 3rem}.global-queue h2{font-size:1.05rem;margin:0}.runtime-jobs{display:grid;gap:1.5rem}.snapshot-note,.runtime-heading{display:flex;flex-wrap:wrap;gap:.75rem;justify-content:space-between;align-items:center}.snapshot-note,.runtime-heading>span,.detail-note,.plugin-status p{color:var(--muted);font-size:.85rem}.queue-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1rem}.queue-card{display:flex;flex-direction:column;gap:1.25rem;text-align:left;padding:1.25rem;border:1px solid var(--line);border-radius:var(--radius-lg,12px);background:var(--surface);color:var(--text);font:inherit}.queue-card:is(button){cursor:pointer}.queue-card.selected{border-color:var(--primary);box-shadow:0 0 0 1px var(--primary)}.queue-title{display:flex;justify-content:space-between;gap:.5rem;font-weight:600}.queue-title>span,.queue-foot{font-size:.78rem;color:var(--muted)}.queue-counts{display:flex;gap:1.5rem}.queue-counts>span{display:grid;gap:.3rem;font-size:.8rem}.queue-counts strong{font-size:1.6rem;font-weight:650}.runtime-section{display:grid;gap:1rem;min-width:0}.runtime-section h2{margin:0;font-size:1.05rem}.runtime-section td small{display:block;color:var(--muted);margin-top:.35rem}.runtime-section .job-error,.queue-error{max-width:26rem;white-space:normal;overflow-wrap:anywhere}.runtime-section .job-error{color:var(--danger)}.detail-note{line-height:1.7;margin:0}.plugin-status{padding:1.25rem;border:1px solid var(--line);border-radius:12px;grid-template-columns:repeat(2,minmax(0,1fr));align-items:center}.plugin-status .detail-note{grid-column:1/-1}.plugin-queue-summary{display:flex;flex-wrap:wrap;gap:1.25rem;color:var(--muted);font-size:.85rem}.plugin-queue-summary strong{color:var(--text)}.task-link{white-space:nowrap}@media(max-width:1000px){.queue-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.spool-card{grid-column:1/-1}}@media(max-width:600px){.queue-grid,.plugin-status{grid-template-columns:1fr}.spool-card{grid-column:auto}.queue-counts{justify-content:space-between}.snapshot-note{font-size:.78rem}.runtime-heading{align-items:flex-start}.queue-card{padding:1rem}}
</style>
<style scoped>
.queue-grid { grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr)); }
.queue-card { gap: .8rem; padding: 1rem; }
.queue-counts strong { font-size: 1.35rem; }
.queue-focused { gap: 1rem; }
.focused-summary { display: flex; flex-wrap: wrap; gap: .5rem 1.25rem; padding-bottom: .75rem; border-bottom: 1px solid var(--line); font-size: .85rem; color: var(--muted); }
.focused-summary strong { color: var(--text); }
.runtime-jobs:not(.queue-focused) { gap: 1rem; }
.runtime-meta { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: .5rem; color: var(--muted); font-size: .82rem; }
.runtime-meta > span { display: inline-flex; align-items: center; gap: .35rem; }
.runtime-error { display: flex; flex-wrap: wrap; align-items: center; gap: .4rem .75rem; padding: .55rem .75rem; border: 1px solid var(--danger); border-radius: 8px; color: var(--danger); font-size: .82rem; }
.runtime-issue { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; color: var(--warning); }
.runtime-issue details { position: relative; color: var(--muted); }
.runtime-issue summary { cursor: pointer; }
.runtime-issue details[open] { flex-basis: 100%; }
.runtime-issue p { margin: .25rem 0; }
.registered-section { display: grid; gap: .55rem; min-width: 0; }
.registered-section h2 { margin: 0; font-size: 1.05rem; }
.registry-unavailable { margin: .5rem 0; color: var(--muted); font-size: .85rem; }
.registered-list { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.registered-row { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(70px, auto) minmax(85px, auto) auto; gap: .75rem; align-items: center; padding: .65rem .8rem; border-bottom: 1px solid var(--line); }
.registered-row:last-child { border-bottom: 0; }
.registered-name { display: grid; gap: .12rem; min-width: 0; }
.registered-name strong { overflow-wrap: anywhere; font-size: .9rem; }
.registered-name small,.registered-cadence,.registered-last,.registered-unknown { color: var(--muted); font-size: .78rem; }
.registered-last { display: inline-flex; align-items: center; gap: .25rem; }
.execution-strip { display: flex; flex-wrap: wrap; align-items: center; gap: .35rem 1.1rem; padding: .55rem .75rem; border: 1px solid var(--line); border-radius: 8px; font-size: .82rem; }
.execution-strip span { color: var(--muted); }
.queue-grid .queue-card { gap: .45rem; padding: .8rem; }
.queue-grid .queue-counts { flex-wrap: wrap; gap: .4rem .9rem; }
.queue-grid .queue-counts > span { display: inline-flex; align-items: baseline; gap: .25rem; }
.queue-grid .queue-counts strong { font-size: 1.15rem; }
.queue-grid .spool-card { grid-column: auto; }
.queue-task { display: grid; justify-items: start; gap: .25rem; min-width: 0; }
.queue-error-trigger { padding: 0; border: 0; background: transparent; color: var(--danger); font: inherit; font-size: .75rem; cursor: pointer; }
.queue-error-trigger:hover { text-decoration: underline; text-underline-offset: 2px; }
.queue-error-trigger:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; border-radius: 2px; }
.runtime-diagnostics { min-width: 0; border-top: 1px solid var(--line); padding-top: .6rem; }
.runtime-diagnostics summary { cursor: pointer; font-weight: 600; font-size: .85rem; }
.runtime-diagnostics[open] { display: grid; gap: 1rem; }
@media (max-width: 600px) {
  .queue-grid { grid-template-columns: 1fr; }
  .focused-summary { justify-content: space-between; }
  .registered-row { grid-template-columns: minmax(0, 1fr) auto; }
  .registered-last { grid-column: 1 / -1; }
}
</style>
