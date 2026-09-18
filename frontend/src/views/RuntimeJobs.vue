<template>
  <section class="standard-page runtime-jobs">
    <PageHeader title="后台任务" description="查看执行周期、最近结果与队列积压，追踪后台工作的运行情况。" eyebrow="Operations">
      <template #actions><RouterLink class="task-link" to="/admin/tasks">运营任务</RouterLink><PageRefreshButton label="刷新后台任务" :loading="loading" @click="refresh" /></template>
    </PageHeader>
    <PageAlert v-if="error" tone="danger" title="运行状态未更新">{{ error }} 已有数据保留为上次快照。</PageAlert>
    <p v-if="!data && loading" role="status">正在读取运行状态…</p>
    <template v-if="data">
      <div class="snapshot-note"><span>最近更新 <TimeBadge :value="data.as_of" /></span><span>每 15 秒刷新 · 任务执行记录持久保存</span></div>
      <section v-if="data.execution_queue" class="queue-card global-queue" aria-label="全局执行队列">
        <div class="runtime-heading"><h2>全局执行队列</h2><span>内置与插件共用 · 执行上限 {{ data.execution_concurrency }} 个</span></div>
        <div class="queue-counts"><span><strong>{{ data.execution_queue.pending }}</strong>等待执行</span><span><strong>{{ data.execution_queue.running }}</strong>执行中</span><span><strong>{{ data.execution_queue.delayed }}</strong>延后执行</span><span><strong>{{ data.execution_queue.unknown }}</strong>结果待核验</span></div>
        <p v-if="data.execution_queue.maintenance_reserved">维护任务已预约独占执行，普通任务暂不领取；结果待核验时需先完成核验。</p>
		<p v-if="data.execution_queue.pending_limit">待执行任务上限 {{ data.execution_queue.pending_limit }}，其中插件合计 {{ data.execution_queue.plugin_pending_limit }}、单插件 {{ data.execution_queue.plugin_owner_pending_limit }}。达到上限后暂停接收新任务，已接收任务继续执行。</p>
      </section>
      <div class="runtime-heading"><h2>业务待处理</h2><span>由统一任务服务领取处理；事件数、业务项数与执行次数分别统计</span></div>
      <section class="queue-grid" aria-label="业务待处理概览">
        <button v-for="queue in data.queues" :key="queue.id" class="queue-card" :class="{ selected: selected === queue.id }" type="button" :aria-pressed="selected === queue.id" @click="selectQueue(queue.id)">
          <span class="queue-title">{{ queue.name }}<span>查看队列 →</span></span>
          <span v-if="queue.id === 'registration_messages'" class="queue-counts"><span><strong>{{ queue.pending }}</strong>待处理事件</span></span>
          <span v-else class="queue-counts"><span><strong>{{ queue.pending }}</strong>等待执行</span><span><strong>{{ queue.running }}</strong>执行中</span><span><strong>{{ queue.delayed }}</strong>延后执行</span></span>
          <span v-if="queue.id === 'registration_messages'" class="queue-foot">最早事件 <TimeBadge :value="queue.oldest_at" /></span>
          <span v-else class="queue-foot">租约失效 {{ queue.stale }}<template v-if="queue.id === 'admin_tasks'"> · 未入队 {{ queue.drafts }} · 失败 {{ queue.failed }}</template></span>
        </button>
        <div class="queue-card spool-card"><span class="queue-title">流量事件入账</span><template v-if="data.runtime.event_spool"><span class="queue-counts"><span><strong>{{ data.runtime.event_spool.pending_events }}</strong>待入账事件</span><span><strong>{{ bytes(data.runtime.event_spool.pending_bytes) }}</strong>待处理数据</span></span><span class="queue-foot">最早事件 <TimeBadge :value="data.runtime.event_spool.oldest_event_at" /> · 存储压力 {{ pressure(data.runtime.event_spool.pressure_level) }}</span></template><p v-else>此实例未启用事件队列</p></div>
      </section>
      <section class="runtime-section">
        <div class="runtime-heading"><h2>任务执行情况</h2><span>共享执行名额 {{ data.execution_concurrency || 4 }} 个 · 外部操作最多占用 {{ data.external_execution_concurrency || 3 }} 个</span></div>
        <DataTable caption="后台任务执行情况" :row-count="data.jobs.length" :min-width="1000">
          <thead><tr><th>任务</th><th>执行周期</th><th>运行状态</th><th>最近结果</th><th>最近完成 / 耗时</th><th>下次扫描</th><th>执行 / 失败</th></tr></thead>
          <tbody><tr v-for="job in data.jobs" :key="job.id"><td><strong>{{ job.name }}</strong><small v-if="job.last_error" class="job-error">{{ job.last_error }}</small></td><td>{{ interval(job) }}<small v-if="job.timezone">{{ job.timezone }} · {{ job.misfire_policy === 'skip' ? '错过跳过' : '错过合并一次' }}</small></td><td><StatusBadge :tone="tone(job.state)">{{ state(job.state) }}<template v-if="job.running"> · {{ job.running }}</template></StatusBadge></td><td><StatusBadge :tone="tone(job.last_result)">{{ state(job.last_result) }}</StatusBadge></td><td><TimeBadge :value="job.last_finished_at" /><small v-if="job.last_finished_at">{{ duration(job.last_duration_ms) }}</small></td><td><TimeBadge v-if="job.next_scan_at" :value="job.next_scan_at" /><span v-else>{{ job.state === 'running' ? '已保留下次计划' : '等待事件或扫描' }}</span></td><td>{{ job.runs }} / {{ job.failures }}<small v-if="job.missed_runs">跳过 {{ job.missed_runs }}</small></td></tr></tbody>
        </DataTable>
        <p v-if="!data.jobs.length">此实例尚无已注册的后台执行器。</p>
        <p class="detail-note">证书续期扫描只负责触发，签发结果请在证书管理查看。周期任务按持久化计划时间推进，不因本轮耗时漂移；错过多个周期时按任务政策跳过或合并执行一次。队列任务按事件唤醒或轮询领取。最近成功表示该轮处理成功，不代表队列已清空。</p>
      </section>
      <section class="runtime-section" aria-label="插件任务">
        <div class="runtime-heading"><h2>插件任务</h2><span>来自插件注册 · 每插件串行，与内置任务共用 {{ data.execution_concurrency || data.plugin_task_concurrency || 4 }} 个执行名额</span></div>
        <p v-if="!data.plugin_host" class="detail-note">插件宿主不可用，暂时无法读取注册任务。</p>
        <template v-else-if="pluginTasks.length">
          <div class="plugin-queue-summary" aria-label="插件任务队列状态"><span>已注册 <strong>{{ pluginTasks.length }}</strong></span><span>到期等待 <strong>{{ pluginTasks.filter(task => task.state === 'queued').length }}</strong></span><span>执行中 <strong>{{ pluginTasks.reduce((sum, task) => sum + task.running, 0) }}</strong></span><span>最近失败或中断 <strong>{{ pluginTasks.filter(task => ['failed', 'interrupted', 'unknown'].includes(task.last_result)).length }}</strong></span></div>
          <DataTable caption="插件注册任务" :row-count="pluginTasks.length" :min-width="1000">
            <thead><tr><th>任务 / 所属插件</th><th>执行周期 / 超时</th><th>运行状态</th><th>最近结果</th><th>最近完成 / 耗时</th><th>下次计划</th><th>执行 / 失败</th></tr></thead>
            <tbody><tr v-for="task in pluginTaskPage" :key="task.id"><td><strong>{{ task.name }}</strong><small><RouterLink :to="`/admin/plugins/${task.plugin_id}`">{{ task.plugin_name }} · {{ task.plugin_id }}</RouterLink></small><small v-if="task.last_error" class="job-error">{{ task.last_error }}</small></td><td>{{ interval(task) }}<small>超时 {{ task.timeout_seconds }} 秒</small></td><td><StatusBadge :tone="tone(task.state)">{{ state(task.state) }}</StatusBadge></td><td><StatusBadge :tone="tone(task.last_result)">{{ state(task.last_result) }}</StatusBadge></td><td><TimeBadge :value="task.last_finished_at" /><small v-if="task.last_finished_at">{{ duration(task.last_duration_ms) }}</small></td><td><TimeBadge v-if="task.next_scan_at" :value="task.next_scan_at" /><span v-else>{{ task.running ? '本轮结束后安排' : '等待启用或调度' }}</span></td><td>{{ task.runs }} / {{ task.failures }}</td></tr></tbody>
          </DataTable>
          <TablePager :total="pluginTasks.length" :offset="pluginOffset" :limit="25" :page-sizes="[25]" @change="value => pluginOffset = value.offset" />
        </template>
        <p v-else class="detail-note">尚无插件注册任务。插件需要声明任务能力与周期，宿主才能调度和记录；插件内部自行启动的定时器不会自动出现在这里。</p>
        <p v-if="pluginTasks.length" class="detail-note">任务计划与执行记录持久保存，重启后继续读取。禁用或版本变化会中断旧执行；超时或失联可能产生待核验结果，此时暂停同一资源的后续工作。</p>
      </section>
      <section class="runtime-section" aria-label="队列明细">
        <div class="runtime-heading"><h2>{{ data.queues.find(queue => queue.id === selected)?.name || '任务' }}队列</h2><span>等待、执行中、延后及待处理异常</span></div>
        <PageAlert v-if="queueError" tone="danger" title="队列明细未更新">{{ queueError }}</PageAlert>
        <DataTable v-if="queuePage?.items.length" caption="队列明细" :row-count="queuePage.total" :min-width="800">
          <thead><tr><th>任务 / 目标</th><th>状态</th><th v-if="selected !== 'registration_messages'">尝试次数</th><th>{{ selected === 'registration_messages' ? '事件时间' : '计划领取时间' }}</th><th v-if="selected !== 'registration_messages'">租约到期</th><th v-if="selected !== 'registration_messages'">最近错误</th></tr></thead>
          <tbody><tr v-for="item in queuePage.items" :key="item.id"><td><span v-if="selected === 'registration_messages'">账户 #{{ item.id }} · 注册事件</span><RouterLink v-else :to="selected === 'admin_tasks' ? `/admin/tasks?task=${item.id}` : `/admin/nodes?node=${item.id}`">{{ selected === 'admin_tasks' ? '任务' : '节点' }} #{{ item.id }}</RouterLink></td><td><StatusBadge :tone="tone(item.state)">{{ selected === 'registration_messages' ? '待处理' : state(item.state) }}</StatusBadge></td><td v-if="selected !== 'registration_messages'">{{ item.attempts }}</td><td><TimeBadge :value="selected === 'registration_messages' ? item.created_at : item.next_attempt_at" /></td><td v-if="selected !== 'registration_messages'"><TimeBadge :value="item.lease_until" /></td><td v-if="selected !== 'registration_messages'" class="queue-error">{{ item.last_error || '—' }}</td></tr></tbody>
        </DataTable>
        <p v-else-if="!queueLoading && !queueError">此队列暂无待处理任务。</p><p v-if="queueLoading" role="status">正在读取队列…</p>
        <TablePager v-if="queuePage" :total="queuePage.total" :offset="offset" :limit="25" :page-sizes="[25]" :loading="queueLoading" @change="changePage" />
      </section>
      <RuntimeJobHistory :as-of="data.as_of" :names="Object.fromEntries([...data.jobs, ...pluginTasks].map(job => [job.id, job.name]))" @resolved="refresh" />
      <section class="runtime-section plugin-status"><div><h2>插件通讯</h2><p>宿主每 10 秒续约与检查；短暂数据库故障会在有效租约内重试。</p></div><template v-if="data.plugin_host"><StatusBadge :tone="data.plugin_host.state === 'active' ? 'success' : 'warning'">{{ data.plugin_host.state === 'active' ? '持有运行租约' : '等待接管 / 恢复' }}</StatusBadge><span>租约到期 <TimeBadge :value="data.plugin_host.lease_until" /></span><span>累计续约失败 {{ data.plugin_host.renewal_failures }}</span></template><span v-else>插件宿主不可用</span><RouterLink to="/admin/plugins">查看各插件状态 →</RouterLink><p class="detail-note">宿主租约表示执行权有效，具体插件的配置、进程及业务连通性请在插件管理中检查。</p></section>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { fetchRuntimeJobs, fetchRuntimeQueue, type RuntimeJobs, type RuntimeJob, type QueuePage } from '../api/runtimeJobs'
import RuntimeJobHistory from '../components/RuntimeJobHistory.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import PageAlert from '../components/PageAlert.vue'
import DataTable from '../components/DataTable.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TablePager from '../components/TablePager.vue'
const data = ref<RuntimeJobs | null>(null), queuePage = ref<QueuePage | null>(null)
const loading = ref(false), queueLoading = ref(false), error = ref(''), queueError = ref('')
const selected = ref('admin_tasks'), offset = ref(0), pluginOffset = ref(0)
const pluginTasks = computed(() => data.value?.plugin_tasks || [])
const pluginTaskPage = computed(() => pluginTasks.value.slice(Math.min(pluginOffset.value, Math.max(0, Math.ceil(pluginTasks.value.length / 25) - 1) * 25), pluginOffset.value + 25))
let controller: AbortController | undefined, queueController: AbortController | undefined, timer: ReturnType<typeof setInterval> | undefined
const labels: Record<string, string> = { idle: '等待下轮', running: '执行中', paused: '维护暂停', stopped: '已停止', succeeded: '成功', failed: '失败', never: '尚未执行', pending: '等待执行', delayed: '延后执行', stale: '租约失效', draft: '未入队', queued: '到期等待', unknown: '结果待核验', yielded: '继续处理', disabled: '已禁用', unavailable: '不可执行', interrupted: '已中断', canceled: '已取消' }
function state(value: string) { return labels[value] || value }
function tone(value: string): 'neutral' | 'success' | 'danger' | 'warning' | 'info' { return value === 'succeeded' ? 'success' : value === 'failed' ? 'danger' : ['unknown', 'stale', 'paused', 'stopped', 'interrupted', 'unavailable'].includes(value) ? 'warning' : value === 'running' ? 'info' : 'neutral' }
function duration(ms: number) { return ms < 1000 ? `${ms} 毫秒` : `${(ms / 1000).toFixed(1)} 秒` }
function interval(job: RuntimeJob) { const n = job.interval_seconds; const value = n >= 3600 ? `${n / 3600} 小时` : n >= 60 ? `${n / 60} 分钟` : `${n} 秒`; return n <= 0 ? (job.kind === 'on_demand' ? '按需执行' : '事件驱动') : job.kind === 'queue' ? `唤醒 / ${value}轮询` : `每 ${value}` }
function bytes(n: number) { return n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB` }
function pressure(value: string) { return ({ normal: '正常', warning: '预警', compact: '压缩中', emergency: '紧急' } as Record<string,string>)[value] || value }
async function refreshQueue() {
  queueController?.abort(); const current = new AbortController(); queueController = current; queueLoading.value = true
  try { const result = await fetchRuntimeQueue(selected.value, offset.value, current.signal); if (!current.signal.aborted) { queuePage.value = result; queueError.value = '' } }
  catch { if (!current.signal.aborted) queueError.value = '读取失败，请稍后重试。' }
  finally { if (queueController === current) queueLoading.value = false }
}
async function refresh() {
  if (loading.value) return
  loading.value = true; const current = new AbortController(); controller = current
  try { const result = await fetchRuntimeJobs(current.signal); if (!current.signal.aborted) { data.value = result; error.value = '' } }
  catch { if (!current.signal.aborted) error.value = '暂时无法读取后台执行情况。' }
  finally { if (!current.signal.aborted) loading.value = false }
  if (!current.signal.aborted) await refreshQueue()
}
function selectQueue(id: string) { if (id === selected.value) return; selected.value = id; offset.value = 0; queuePage.value = null; queueError.value = ''; void refreshQueue() }
function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; void refreshQueue() }
onMounted(() => { void refresh(); timer = setInterval(() => { if (!document.hidden && !queueLoading.value) void refresh() }, 15000) })
onBeforeUnmount(() => { clearInterval(timer); controller?.abort(); queueController?.abort() })
</script>
<style scoped>
.global-queue .queue-counts{flex-wrap:wrap;gap:1.5rem 3rem}.global-queue h2{font-size:1.05rem;margin:0}.runtime-jobs{display:grid;gap:1.5rem}.snapshot-note,.runtime-heading{display:flex;flex-wrap:wrap;gap:.75rem;justify-content:space-between;align-items:center}.snapshot-note,.runtime-heading>span,.detail-note,.plugin-status p{color:var(--muted);font-size:.85rem}.queue-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1rem}.queue-card{display:flex;flex-direction:column;gap:1.25rem;text-align:left;padding:1.25rem;border:1px solid var(--line);border-radius:var(--radius-lg,12px);background:var(--surface);color:var(--text);font:inherit}.queue-card:is(button){cursor:pointer}.queue-card.selected{border-color:var(--primary);box-shadow:0 0 0 1px var(--primary)}.queue-title{display:flex;justify-content:space-between;gap:.5rem;font-weight:600}.queue-title>span,.queue-foot{font-size:.78rem;color:var(--muted)}.queue-counts{display:flex;gap:1.5rem}.queue-counts>span{display:grid;gap:.3rem;font-size:.8rem}.queue-counts strong{font-size:1.6rem;font-weight:650}.runtime-section{display:grid;gap:1rem;min-width:0}.runtime-section h2{margin:0;font-size:1.05rem}.runtime-section td small{display:block;color:var(--muted);margin-top:.35rem}.runtime-section .job-error,.queue-error{max-width:26rem;white-space:normal;overflow-wrap:anywhere}.runtime-section .job-error{color:var(--danger,#c53f45)}.detail-note{line-height:1.7;margin:0}.plugin-status{padding:1.25rem;border:1px solid var(--line);border-radius:12px;grid-template-columns:repeat(2,minmax(0,1fr));align-items:center}.plugin-status .detail-note{grid-column:1/-1}.plugin-queue-summary{display:flex;flex-wrap:wrap;gap:1.25rem;color:var(--muted);font-size:.85rem}.plugin-queue-summary strong{color:var(--text)}.task-link{white-space:nowrap}@media(max-width:1000px){.queue-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.spool-card{grid-column:1/-1}}@media(max-width:600px){.queue-grid,.plugin-status{grid-template-columns:1fr}.spool-card{grid-column:auto}.queue-counts{justify-content:space-between}.snapshot-note{font-size:.78rem}.runtime-heading{align-items:flex-start}.queue-card{padding:1rem}}
</style>
