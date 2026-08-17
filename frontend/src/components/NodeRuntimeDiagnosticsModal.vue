<template>
  <ModalDialog
    :open="open"
    title="运行诊断"
    :description="nodeName ? `${nodeName} · 检查当前分配的 Zero 协议服务` : '检查当前分配的 Zero 协议服务'"
    size="lg"
    :busy="loading"
    fixed-body
    @close="emit('close')"
  >
    <div class="diagnostic-stack">
      <PageAlert v-if="error" tone="danger" title="运行诊断失败">{{ error }}</PageAlert>
      <PageAlert v-if="!sshReady" tone="warning" title="需要已验证的 SSH 通道">先完成节点 SSH 配置和验证，再运行诊断。</PageAlert>

      <div v-if="loading" class="diagnostic-loading">
        <UiIcon name="activity" />
        <div><strong>正在检查运行状态</strong><span>确认 SSH、Zero 和 Zboard 当前分配到该节点的协议服务。</span></div>
      </div>

      <template v-else-if="snapshot">
        <section class="diagnostic-summary">
          <div>
            <StatusBadge :tone="statusTone(snapshot.status)" :icon="snapshot.status === 'healthy' ? 'check' : 'alert'">
              {{ statusLabel(snapshot.status) }}
            </StatusBadge>
            <strong>{{ snapshot.status === 'healthy' ? '当前已分配服务运行正常' : '发现需要检查的服务状态' }}</strong>
          </div>
          <UiButton variant="secondary" size="sm" type="button" :disabled="!sshReady" @click="run">
            <UiIcon name="refresh" />重新诊断
          </UiButton>
        </section>

        <section class="diagnostic-facts" aria-label="节点运行状态">
          <div><span>SSH</span><StatusBadge :tone="statusTone(snapshot.checks.ssh)" :icon="snapshot.checks.ssh === 'healthy' ? 'check' : 'alert'">{{ statusLabel(snapshot.checks.ssh) }}</StatusBadge></div>
          <div><span>Zero</span><StatusBadge :tone="statusTone(snapshot.checks.zero)" :icon="snapshot.checks.zero === 'healthy' ? 'check' : 'alert'">{{ statusLabel(snapshot.checks.zero) }}</StatusBadge></div>
        </section>

        <section class="diagnostic-section">
          <header><div><strong>协议服务</strong><span>只检查 Zboard 当前启用并分配到该节点的协议，不展示主机端口或其他宿主机信息。</span></div></header>
          <DataTable v-if="snapshot.protocols.length" caption="当前分配协议的运行状态" :row-count="snapshot.protocols.length" :min-width="520">
            <thead><tr><th>服务</th><th>协议</th><th>状态</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="protocol in snapshot.protocols" :key="`${protocol.name}-${protocol.protocol}`">
                <td><strong>{{ protocol.name || protocolLabel(protocol.protocol) }}</strong></td>
                <td>{{ protocolLabel(protocol.protocol) }}</td>
                <td><StatusBadge :tone="statusTone(protocol.status)" :icon="protocol.status === 'healthy' ? 'check' : 'alert'">{{ statusLabel(protocol.status) }}</StatusBadge></td>
                <td>{{ protocol.status === 'healthy' ? '运行正常' : reasonLabel(protocol.reason) }}</td>
              </tr>
            </tbody>
          </DataTable>
          <EmptyState v-else icon="activity" title="没有需要诊断的协议服务" description="当前节点没有启用的协议分配。" />
        </section>
      </template>

      <EmptyState v-else-if="sshReady && !error" icon="activity" title="尚未运行诊断" description="检查当前分配到该节点的 Zero 协议服务状态。">
        <template #actions><UiButton type="button" @click="run"><UiIcon name="activity" />运行诊断</UiButton></template>
      </EmptyState>
    </div>

    <template #footer="{ requestClose }">
      <UiButton variant="secondary" type="button" :disabled="loading" @click="requestClose">关闭</UiButton>
      <UiButton type="button" :loading="loading" :disabled="!sshReady" @click="run"><UiIcon name="refresh" />{{ snapshot ? '重新诊断' : '运行诊断' }}</UiButton>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { runNodeDiagnostics, type NodeDiagnosticReason, type NodeDiagnosticSnapshot, type NodeDiagnosticStatus } from '../api/nodeDiagnostics'
import { normalizeApiErrorMessage } from '../utils/apiError'
import DataTable from './DataTable.vue'
import EmptyState from './EmptyState.vue'
import ModalDialog from './ModalDialog.vue'
import PageAlert from './PageAlert.vue'
import StatusBadge from './StatusBadge.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'

const props = defineProps<{
  open: boolean
  nodeId: number
  nodeName?: string
  sshReady: boolean
}>()
const emit = defineEmits<{ close: [] }>()
const loading = ref(false)
const error = ref('')
const snapshot = ref<NodeDiagnosticSnapshot | null>(null)
let requestSequence = 0

watch(() => [props.open, props.nodeId] as const, ([open, nodeId], previous) => {
  if (!open || !nodeId) return
  if (!previous || !previous[0] || previous[1] !== nodeId) {
    snapshot.value = null
    error.value = ''
    if (props.sshReady) void run()
  }
}, { immediate: true })

async function run() {
  if (!props.nodeId || !props.sshReady || loading.value) return
  const sequence = ++requestSequence
  loading.value = true
  error.value = ''
  try {
    const result = await runNodeDiagnostics(props.nodeId)
    if (sequence === requestSequence) snapshot.value = result
  } catch (cause: any) {
    if (sequence === requestSequence) error.value = normalizeApiErrorMessage(cause, '节点运行诊断失败，请确认 SSH 和 Zero 服务。')
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function statusLabel(value: NodeDiagnosticStatus) {
  return value === 'healthy' ? '正常' : '异常'
}

function statusTone(value: NodeDiagnosticStatus): 'success' | 'danger' {
  return value === 'healthy' ? 'success' : 'danger'
}

function protocolLabel(value: string) {
  const labels: Record<string, string> = {
    shadowsocks: 'Shadowsocks',
    hysteria2: 'Hysteria2',
    trojan: 'Trojan',
    vless: 'VLESS',
    vmess: 'VMess',
    mieru: 'Mieru',
  }
  return labels[value?.toLowerCase()] || value || '未知协议'
}

function reasonLabel(reason?: NodeDiagnosticReason) {
  switch (reason) {
    case 'ssh_unavailable': return 'SSH 通道不可用'
    case 'zero_unavailable': return 'Zero 服务不可用'
    case 'listener_unavailable': return '服务监听异常'
    case 'config_invalid': return '服务配置异常'
    case 'not_checked': return '未完成检查'
    default: return '运行状态异常'
  }
}
</script>

<style scoped>
.diagnostic-stack{display:grid;gap:14px;min-width:0}
.diagnostic-loading{min-height:150px;display:flex;align-items:center;justify-content:center;gap:12px;color:var(--muted)}
.diagnostic-loading>.ui-icon{width:26px;height:26px;color:var(--primary)}
.diagnostic-loading>div{display:grid;gap:4px}.diagnostic-loading strong{color:var(--text-strong);font-size:12px}.diagnostic-loading span{max-width:520px;font-size:10px;line-height:1.55}
.diagnostic-summary{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;padding:13px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}
.diagnostic-summary>div{display:grid;justify-items:start;gap:6px}.diagnostic-summary strong{color:var(--text-strong);font-size:12px}
.diagnostic-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}
.diagnostic-facts>div{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:11px;border:1px solid var(--line);border-radius:9px;background:var(--surface)}
.diagnostic-facts span{color:var(--muted);font-size:10px}
.diagnostic-section{display:grid;gap:10px}.diagnostic-section>header{display:flex;align-items:flex-start;justify-content:space-between;gap:10px;padding-bottom:8px;border-bottom:1px solid var(--line)}
.diagnostic-section>header>div{display:grid;gap:3px}.diagnostic-section>header strong{color:var(--text-strong);font-size:11px}.diagnostic-section>header span{color:var(--muted);font-size:9px;line-height:1.5}
@media(max-width:640px){.diagnostic-summary{align-items:stretch;flex-direction:column}.diagnostic-facts{grid-template-columns:1fr}}
</style>
