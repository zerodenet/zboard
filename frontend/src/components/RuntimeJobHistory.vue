<template>
  <section class="history" aria-label="任务执行历史">
    <h2>执行记录</h2>
    <PageAlert v-if="error" tone="danger" title="执行记录未更新">{{ error }}</PageAlert>
    <p v-if="notice" role="status">{{ notice }}</p>
    <DataTable caption="任务执行历史" :row-count="page?.total || 0" :min-width="800">
      <thead><tr><th>任务</th><th>来源</th><th>状态</th><th>尝试</th><th>创建 / 完成</th><th>处理</th></tr></thead>
      <tbody><template v-for="run in page?.items || []" :key="run.id"><tr>
        <td>{{ names[run.handler] || taskName(run.handler) }}</td><td>{{ run.owner === 'system' ? 'ZBoard' : run.owner.replace(/^plugin:/, '') }}</td>
        <td><StatusBadge :tone="run.state === 'unknown' ? 'warning' : run.state === 'failed' ? 'danger' : 'neutral'">{{ labels[run.state] || run.state }}</StatusBadge></td>
        <td>{{ run.attempts || 0 }} / {{ run.max_attempts || 1 }}<template v-if="run.next_attempt_at"><br /><TimeBadge :value="run.next_attempt_at" /></template></td>
        <td><small v-if="run.planned_at">计划 <TimeBadge :value="run.planned_at" /></small><TimeBadge :value="run.created_at" /><br /><TimeBadge :value="run.finished_at" /></td>
        <td><button v-if="(run.attempts || 0) > 0" type="button" :disabled="attemptLoading === run.id" @click="toggleAttempts(run)">{{ attempts[run.id] ? '收起尝试' : '查看尝试' }}</button><button v-if="cancelable(run)" type="button" :disabled="canceling === run.id" @click="cancel(run)">{{ canceling === run.id ? '请求中…' : '取消任务' }}</button><button v-if="run.state === 'unknown' && run.owner === 'system' && run.handler === 'dns_operation'" type="button" :disabled="inspecting" @click="inspectDNS(run)">核验远端 DNS</button><button v-if="run.state === 'unknown'" type="button" @click="select(run)">记录核验结果</button><span v-if="(run.attempts || 0) === 0 && !cancelable(run) && run.state !== 'unknown'">—</span></td>
      </tr><tr v-if="attempts[run.id]" class="attempt-row"><td colspan="6"><p v-if="attempts[run.id].items.length === 0">尚无执行尝试。</p><ol v-else><li v-for="attempt in attempts[run.id].items" :key="attempt.attempt_number"><strong>第 {{ attempt.attempt_number }} 次</strong> · {{ labels[attempt.state] || attempt.state }} · {{ attempt.worker }} · <TimeBadge :value="attempt.started_at" /> → <TimeBadge :value="attempt.finished_at" /></li></ol></td></tr></template></tbody>
    </DataTable>
    <p v-if="page?.total === 0">暂无执行记录。</p>
    <TablePager v-if="page" :total="page.total" :offset="offset" :limit="25" :page-sizes="[25]" :loading="loading" @change="changePage" />
    <form v-if="selected" ref="formElement" class="resolution" novalidate @submit.prevent="resolve">
      <h3>记录核验结果</h3><p v-if="selected.handler === 'database_migration'">请先检查目标数据库的迁移结果和数据完整性。保存后解除迁移的执行等待，不会自动关闭维护模式或重新复制数据。</p><p v-else>请先核验实际操作结果。保存后解除该资源的等待状态，后续周期可继续执行。</p>
      <FormField v-slot="{ controlAttrs }" label="实际结果" name="runtime-resolution-outcome" :error="resolutionErrors.fields.outcome" required full><UiSelect v-model="outcome" v-bind="controlAttrs" :options="outcomeOptions" /></FormField>
      <FormField v-slot="{ controlAttrs }" label="核验依据" name="runtime-resolution-reason" :error="resolutionErrors.fields.reason" required full><UiTextarea v-model="reason" v-bind="controlAttrs" maxlength="500" rows="3" /></FormField>
      <div><button type="submit" :disabled="saving || !reason.trim()">{{ saving ? '保存中…' : '保存核验结果' }}</button><button type="button" :disabled="saving" @click="selected = null">取消</button></div>
      <PageAlert v-if="resolutionErrors.formError.value" tone="danger" title="核验结果未保存">{{ resolutionErrors.formError.value }}</PageAlert>
    </form>
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { cancelRun, fetchRunAttempts, fetchRunHistory, resolveRun, reconcileDNSRun, type RunAttempts, type RunHistory, type RunRecord } from '../api/runtimeHistory'
import DataTable from './DataTable.vue'
import TablePager from './TablePager.vue'
import TimeBadge from './TimeBadge.vue'
import StatusBadge from './StatusBadge.vue'
import PageAlert from './PageAlert.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'
import FormField from './FormField.vue'
import { useFormErrors } from '../composables/useFormState'
import { collectFieldErrors } from '../utils/validation'
const props = defineProps<{ asOf: string; names: Record<string, string> }>()
const emit = defineEmits<{ resolved: [] }>()
const page = ref<RunHistory | null>(null), offset = ref(0), loading = ref(false), error = ref('')
const selected = ref<RunRecord | null>(null), reason = ref(''), outcome = ref('succeeded'), saving = ref(false)
const attempts = ref<Record<string, RunAttempts>>({}), attemptLoading = ref(''), canceling = ref('')
const formElement = ref<HTMLElement | null>(null), resolutionErrors = useFormErrors()
const inspecting = ref(false), notice = ref('')
let controller: AbortController | undefined
const labels: Record<string, string> = { queued: '等待执行', retry_wait: '等待重试', running: '执行中', cancel_requested: '正在取消', succeeded: '成功', failed: '失败', unknown: '结果待核验', yielded: '继续处理', interrupted: '已中断', canceled: '已取消' }
const outcomeOptions = [{ label: '已确认成功', value: 'succeeded' }, { label: '已确认失败', value: 'failed' }]
async function load() {
  controller?.abort(); const current = new AbortController(); controller = current; loading.value = true
  try { const result = await fetchRunHistory(offset.value, current.signal); if (!current.signal.aborted) { page.value = result; error.value = '' } }
  catch { if (!current.signal.aborted) error.value = '读取失败，请稍后刷新。' }
  finally { if (controller === current) loading.value = false }
}
function taskName(handler: string) { if (handler.startsWith('node_publish_')) return '节点配置发布'; return ({ certificate_operation: '证书签发', dns_operation: 'DNS 同步', dns_reconcile: 'DNS 结果核验', database_migration: '数据库迁移' } as Record<string, string>)[handler] || handler }
function changePage(value: { offset: number }) { offset.value = value.offset; void load() }
function cancelable(run: RunRecord) { return ['queued', 'retry_wait', 'running'].includes(run.state) }
async function toggleAttempts(run: RunRecord) {
  if (attempts.value[run.id]) { const next = { ...attempts.value }; delete next[run.id]; attempts.value = next; return }
  attemptLoading.value = run.id
  try { attempts.value = { ...attempts.value, [run.id]: await fetchRunAttempts(run.id) } }
  catch { error.value = '尝试记录读取失败，请稍后重试。' }
  finally { if (attemptLoading.value === run.id) attemptLoading.value = '' }
}
async function cancel(run: RunRecord) {
  if (canceling.value) return
  canceling.value = run.id
  try { await cancelRun(run.id, '管理员从执行历史请求取消'); await load(); emit('resolved') }
  catch { error.value = '取消请求未保存；任务可能已经结束，请刷新确认。' }
  finally { canceling.value = '' }
}
function select(run: RunRecord) { selected.value = run; reason.value = ''; outcome.value = 'succeeded'; resolutionErrors.clear() }
async function inspectDNS(run: RunRecord) {
  if (inspecting.value) return
  inspecting.value = true; notice.value = ''
  try { await reconcileDNSRun(run.id); await load(); notice.value = '核验任务已入队。将只读检查远端记录，匹配当前配置后更新原任务状态。'; emit('resolved') }
  catch { error.value = '核验任务未提交，请确认远端标识完整、供应商账户可用及原任务仍待核验。' }
  finally { inspecting.value = false }
}
async function resolve() {
	if (!selected.value || saving.value) return
	if (!await resolutionErrors.applyValidation(collectFieldErrors({
		outcome: !['succeeded', 'failed'].includes(outcome.value) && '请选择实际结果。',
		reason: !reason.value.trim() && '请填写核验依据。',
	}), formElement)) return
  saving.value = true
  try { await resolveRun(selected.value.id, outcome.value, reason.value.trim()); selected.value = null; await load(); emit('resolved') }
  catch (cause) { await resolutionErrors.applyApiError(cause, '核验结果未保存，请刷新确认状态后重试。', formElement, { outcome: 'outcome', reason: 'reason' }) }
  finally { saving.value = false }
}
watch(outcome, () => resolutionErrors.clear('outcome')); watch(reason, () => resolutionErrors.clear('reason'))
watch(() => props.asOf, () => void load(), { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
<style scoped>
.history{display:grid;gap:1rem;min-width:0}.history h2{font-size:1.05rem;margin:0}.attempt-row td{background:var(--surface-soft)}.attempt-row ol{display:grid;gap:.35rem;margin:0;padding-left:1.25rem}.resolution{display:grid;gap:1rem;padding:1rem;border:1px solid var(--line);border-radius:12px}.resolution label{display:grid;gap:.5rem}.resolution p{color:var(--muted)}.resolution div{display:flex;gap:.75rem}button{font:inherit}
</style>
