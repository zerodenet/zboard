<template>
  <ModalDialog :open="open" :title="`删除节点 · ${task?.node_name || nodeName}`" description="清理结果逐项保存；关闭窗口不会取消已提交的任务。" @close="$emit('close')">
    <div class="retirement-content">
      <p>删除节点及其协议、节点组和入口关联，同时删除面板管理且归属已确认的供应商 DNS，停止证书和代理池管理。</p>
      <p>节点离线或 SSH 不可用时，远端程序和文件可能无法清理，具体结果会逐项显示。删除不会清除历史计费数据。</p>
      <PageAlert v-if="error || task?.last_error" tone="danger">{{ error || task?.last_error }}</PageAlert>
      <PageAlert v-if="task?.state === 'succeeded' && task.mode !== 'delete'" tone="info">此前的操作已经结束。确认删除后将继续清理供应商 DNS 和系统管理关联。</PageAlert>
      <p v-if="loading" role="status">正在读取任务进度…</p>
      <template v-if="task">
        <p role="status">{{ stateLabel(task.state) }} · 第 {{ task.attempt }} 次提交</p>
        <ol aria-label="节点删除进度">
          <li v-for="step in task.steps" :key="`${step.kind}:${step.id}`">
            <div><strong>{{ step.label }}</strong><StatusBadge :tone="step.state === 'failed' ? 'danger' : step.state === 'succeeded' ? 'success' : step.state === 'unconfirmed' ? 'warning' : 'neutral'">{{ stateLabel(step.state) }}</StatusBadge></div>
            <p v-if="step.message">{{ step.message }}</p>
          </li>
          <li><div><strong>删除节点及系统关联</strong><StatusBadge :tone="completed ? 'success' : 'neutral'">{{ completed ? '已完成' : '等待关联清理完成' }}</StatusBadge></div></li>
        </ol>
        <p v-if="failed">删除尚未完成。请检查失败项目中的供应商权限或资源归属后重试，已完成的步骤会保留进度。</p>
        <p v-if="completed">节点已删除。历史计费数据保留；远端程序和文件的清理结果见上方明细。</p>
      </template>
    </div>
    <template #footer>
      <UiButton variant="secondary" type="button" @click="$emit('close')">{{ active ? '后台继续' : '关闭' }}</UiButton>
      <UiButton v-if="!completed" variant="danger" type="button" :loading="busy" :disabled="loading || busy || active" @click="submit">{{ failed ? '重试未完成项目' : '确认删除节点' }}</UiButton>
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
const mode = ref<RetirementMode>('delete'), task = ref<NodeRetirement | null>(null)
const busy = ref(false), loading = ref(false), error = ref('')
const active = computed(() => Boolean(task.value && ['queued', 'running', 'retry_wait', 'cancel_requested'].includes(task.value.state)))
const completed = computed(() => task.value?.state === 'succeeded' && task.value.mode === 'delete')
const failed = computed(() => Boolean(task.value && ['failed', 'unknown', 'interrupted', 'canceled'].includes(task.value.state)))
let generation = 0, controller = new AbortController(), timer: ReturnType<typeof setTimeout> | undefined, reported = false
function stateLabel(state: string) { return ({ pending: '等待处理', queued: '排队中', running: '处理中', succeeded: '已完成', retained: '历史保留结果', unconfirmed: '远端清理未确认', failed: '未完成', unknown: '结果待核验', interrupted: '执行中断', canceled: '已取消', retry_wait: '等待重试', cancel_requested: '正在取消' } as Record<string, string>)[state] || state }
function stop() { clearTimeout(timer); controller.abort(); generation++; loading.value = false; busy.value = false }
function accept(value: NodeRetirement | null) {
  task.value = value
  if (value?.state === 'succeeded' && value.mode === 'delete' && !reported) { reported = true; emit('completed') }
}
function poll() {
  clearTimeout(timer)
  if (props.open && active.value) timer = setTimeout(() => { void reload() }, 2000)
}
async function reload() {
  const current = generation
  loading.value = true; error.value = ''
  try { const value = await fetchNodeRetirement(props.nodeId, controller.signal); if (current === generation) accept(value) }
  catch (cause) { if (current === generation) error.value = normalizeApiErrorMessage(cause, '无法读取删除进度，请重试。') }
  finally { if (current === generation) { loading.value = false; poll() } }
}
async function submit() {
  if (busy.value || active.value || completed.value) return
  const current = generation
  busy.value = true; error.value = ''
  try { const value = await startNodeRetirement(props.nodeId, mode.value, controller.signal); if (current === generation) accept(value) }
  catch (cause) { if (current === generation) error.value = normalizeApiErrorMessage(cause, '删除任务提交失败，请重试。') }
  finally { if (current === generation) { busy.value = false; poll() } }
}
watch(() => [props.open, props.nodeId], () => {
  stop(); task.value = null; mode.value = 'delete'; error.value = ''; reported = false
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
