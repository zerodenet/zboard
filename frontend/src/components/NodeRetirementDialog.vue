<template>
  <ModalDialog :open="open" :title="`节点退役 · ${task?.node_name || nodeName}`" description="清理结果逐项保存；关闭窗口不会取消已提交的任务。" @close="$emit('close')">
    <div class="retirement-content">
      <fieldset :disabled="busy || active || completed">
        <legend>处理方式</legend>
        <label><input v-model="mode" type="radio" value="retire" name="retirement-mode">退役并清理独占资源</label>
        <p>通过已验证 SSH 卸载该节点的 Zero，清理面板专用证书文件和可确认归属的 DNS。该节点现有代理连接会中断。共享和来源不匹配的资源会保留，不撤销 CA 证书。持久化事件目录保留，避免丢失待投递事件。</p>
        <label><input v-model="mode" type="radio" value="detach" name="retirement-mode">仅移除面板管理</label>
        <p>保留剩余远端进程、文件和供应商 DNS，只移除面板内的节点及关联。已完成的清理无法撤销。</p>
      </fieldset>
      <PageAlert v-if="error || task?.last_error" tone="danger">{{ error || task?.last_error }}</PageAlert>
      <p v-if="loading" role="status">正在读取任务进度…</p>
      <template v-if="task">
        <p role="status">{{ stateLabel(task.state) }} · 第 {{ task.attempt }} 次提交</p>
        <ol aria-label="节点退役进度">
          <li v-for="step in task.steps" :key="`${step.kind}:${step.id}`">
            <div><strong>{{ step.label }}</strong><StatusBadge :tone="step.state === 'failed' ? 'danger' : step.state === 'succeeded' ? 'success' : 'neutral'">{{ stateLabel(step.state) }}</StatusBadge></div>
            <p v-if="step.message">{{ step.message }}</p>
          </li>
          <li><div><strong>移除面板记录及关联</strong><StatusBadge :tone="completed ? 'success' : 'neutral'">{{ completed ? '已完成' : '待确认清理结果' }}</StatusBadge></div></li>
        </ol>
        <p v-if="failed">节点与未完成资源记录已保留。修复连接或权限后可重试；已确认完成的步骤不会重复执行。结果不明的任务需先等待远端命令超时，再重新核对。</p>
        <p v-if="completed">处理完成。历史流量、任务和审计记录仍保留。</p>
      </template>
    </div>
    <template #footer>
      <UiButton variant="secondary" type="button" @click="$emit('close')">{{ active ? '后台继续' : '关闭' }}</UiButton>
      <UiButton v-if="!completed" variant="danger" type="button" :loading="busy" :disabled="loading || busy || active" @click="submit">{{ failed && mode === task?.mode ? '重试未完成步骤' : mode === 'detach' ? '确认仅移除面板管理' : '确认退役并清理' }}</UiButton>
      <UiButton v-if="task" variant="ghost" type="button" :disabled="busy || loading" @click="reload">刷新进度</UiButton>
    </template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { fetchNodeRetirement, startNodeRetirement, type NodeRetirement, type RetirementMode } from '../api/nodeRetirement'
import { normalizeApiErrorMessage } from '../utils/apiError'
import ModalDialog from './ModalDialog.vue'
import UiButton from './UiButton.vue'
import PageAlert from './PageAlert.vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ open: boolean; nodeId: number; nodeName: string }>()
const emit = defineEmits<{ close: []; completed: [] }>()
const mode = ref<RetirementMode>('retire'), task = ref<NodeRetirement | null>(null)
const busy = ref(false), loading = ref(false), error = ref('')
const active = computed(() => Boolean(task.value && ['queued', 'running', 'retry_wait', 'cancel_requested'].includes(task.value.state)))
const completed = computed(() => task.value?.state === 'succeeded')
const failed = computed(() => Boolean(task.value && ['failed', 'unknown', 'interrupted', 'canceled'].includes(task.value.state)))
let generation = 0, controller = new AbortController(), timer: ReturnType<typeof setTimeout> | undefined, reported = false
function stateLabel(state: string) { return ({ pending: '等待处理', queued: '排队中', running: '处理中', succeeded: '已完成', retained: '已保留', failed: '未完成', unknown: '结果待核验', interrupted: '执行中断', canceled: '已取消', retry_wait: '等待重试', cancel_requested: '正在取消' } as Record<string, string>)[state] || state }
function stop() { clearTimeout(timer); controller.abort(); generation++; loading.value = false; busy.value = false }
function accept(value: NodeRetirement | null) {
  task.value = value
  if (value) mode.value = value.mode
  if (value?.state === 'succeeded' && !reported) { reported = true; emit('completed') }
}
function poll() {
  clearTimeout(timer)
  if (props.open && active.value) timer = setTimeout(() => { void reload() }, 2000)
}
async function reload() {
  const current = generation
  loading.value = true; error.value = ''
  try { const value = await fetchNodeRetirement(props.nodeId, controller.signal); if (current === generation) accept(value) }
  catch (cause) { if (current === generation) error.value = normalizeApiErrorMessage(cause, '无法读取退役进度，请重试。') }
  finally { if (current === generation) { loading.value = false; poll() } }
}
async function submit() {
  if (busy.value || active.value || completed.value) return
  const current = generation
  busy.value = true; error.value = ''
  try { const value = await startNodeRetirement(props.nodeId, mode.value, controller.signal); if (current === generation) accept(value) }
  catch (cause) { if (current === generation) error.value = normalizeApiErrorMessage(cause, '退役任务提交失败，请重试。') }
  finally { if (current === generation) { busy.value = false; poll() } }
}
watch(() => [props.open, props.nodeId], () => {
  stop(); task.value = null; mode.value = 'retire'; error.value = ''; reported = false
  if (props.open && props.nodeId) { controller = new AbortController(); void reload() }
}, { immediate: true })
onBeforeUnmount(stop)
</script>
<style scoped>
.retirement-content { display: grid; gap: 16px; }
fieldset { border: 1px solid var(--line); border-radius: 8px; padding: 14px; }
label, li > div { display: flex; align-items: center; gap: 10px; }
p { margin: 8px 0; font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }
ol { margin: 0; padding-left: 22px; }
li { padding: 8px 0; }
li > div { justify-content: space-between; }
</style>
