<template>
  <ModalDialog
    :open="open"
    title="运行诊断"
    :description="nodeName ? `${nodeName} · 对比 Zero 配置期望、本机监听与主机状态` : '对比 Zero 配置期望、本机监听与主机状态'"
    size="xl"
    :busy="loading"
    fixed-body
    @close="emit('close')"
  >
    <div class="diagnostic-stack">
      <PageAlert v-if="error" tone="danger" title="运行诊断失败">{{ error }}</PageAlert>
      <PageAlert v-if="!sshReady" tone="warning" title="需要已验证的 SSH 通道">先完成节点 SSH 配置和验证，再运行主机级诊断。</PageAlert>

      <div v-if="loading" class="diagnostic-loading">
        <UiIcon name="activity" />
        <div><strong>正在采集诊断快照</strong><span>读取 Zero 状态、本机监听、systemd、文件描述符与 conntrack，并执行有限的 TCP 可达性检查。</span></div>
      </div>

      <template v-else-if="snapshot">
        <section class="diagnostic-summary">
          <div>
            <StatusBadge :tone="classificationTone(snapshot.classification)" :icon="classificationIcon(snapshot.classification)">
              {{ classificationLabel(snapshot.classification) }}
            </StatusBadge>
            <strong>{{ snapshot.summary }}</strong>
            <span>采样 <TimeBadge :value="snapshot.captured_at" /> · SSH {{ snapshot.latency_ms }} ms</span>
          </div>
          <UiButton variant="secondary" size="sm" type="button" :disabled="!sshReady" @click="run">
            <UiIcon name="refresh" />重新诊断
          </UiButton>
        </section>

        <PageAlert v-for="warning in snapshot.warnings || []" :key="warning" tone="warning" title="诊断提示">{{ warning }}</PageAlert>
        <PageAlert v-if="!snapshot.capabilities.native_listener_health" tone="info" title="监听健康由主机事实确认">
          Zero 状态快照用于读取配置 listener 与运行实例；实际端口是否存在以本次 SSH 采集的 ss 监听表为准，不把控制面在线直接等同于数据面健康。
        </PageAlert>

        <section class="diagnostic-facts" aria-label="运行诊断事实">
          <div><span>Zero 状态快照</span><strong>{{ snapshot.capabilities.native_runtime_snapshot ? '可用' : '不可用' }}</strong><small>{{ snapshot.runtime.control_status || 'unknown' }}</small></div>
          <div><span>Core instance</span><strong class="mono">{{ snapshot.runtime.core_instance_id || '—' }}</strong><small>PID {{ snapshot.runtime.pid || snapshot.service.main_pid || '—' }}</small></div>
          <div><span>配置 revision</span><strong>{{ snapshot.runtime.config_revision || '—' }}</strong><small>{{ snapshot.runtime.engine_version || '未读取版本' }}</small></div>
          <div><span>systemd</span><strong>{{ snapshot.service.active_state || 'unknown' }} / {{ snapshot.service.sub_state || 'unknown' }}</strong><small>exit {{ snapshot.service.exec_main_status ?? '—' }}</small></div>
          <div><span>文件描述符</span><strong>{{ ratioValue(snapshot.resources.fd_count, snapshot.resources.fd_soft_limit) }}</strong><small>{{ ratioPercent(snapshot.resources.fd_ratio) }}</small></div>
          <div><span>conntrack</span><strong>{{ ratioValue(snapshot.resources.conntrack_count, snapshot.resources.conntrack_max) }}</strong><small>{{ ratioPercent(snapshot.resources.conntrack_ratio) }}</small></div>
        </section>

        <section class="diagnostic-section">
          <header><div><strong>预期监听与实际状态</strong><span>来自 Zero 配置快照的 listener 逐项与主机 ss 结果比对。</span></div></header>
          <DataTable v-if="snapshot.expected_listeners.length" caption="Zero 配置监听与主机实际监听对比" :row-count="snapshot.expected_listeners.length" :min-width="760">
            <thead><tr><th>入站</th><th>协议</th><th>网络</th><th>监听地址</th><th>本机状态</th><th>外部 TCP</th></tr></thead>
            <tbody>
              <tr v-for="listener in snapshot.expected_listeners" :key="`${listener.tag}-${listener.port}`">
                <td><div class="cell-title"><strong>{{ listener.tag || '未命名' }}</strong><span>:{{ listener.port }}</span></div></td>
                <td>{{ listener.protocol || '—' }}</td>
                <td class="mono">{{ listener.networks.join(' + ').toUpperCase() }}</td>
                <td class="mono">{{ listener.address || '*' }}:{{ listener.port }}</td>
                <td>
                  <StatusBadge :tone="listener.present ? 'success' : 'danger'" :icon="listener.present ? 'check' : 'alert'">
                    {{ listener.present ? '监听正常' : `缺少 ${(listener.missing_networks || []).join(' / ').toUpperCase()}` }}
                  </StatusBadge>
                </td>
                <td><StatusBadge :tone="reachabilityTone(listener.external_reachability)">{{ reachabilityLabel(listener) }}</StatusBadge></td>
              </tr>
            </tbody>
          </DataTable>
          <EmptyState v-else icon="activity" title="没有可比对的监听信息" description="当前 Zero 状态快照未提供配置 listener，因此诊断不会判定数据面健康。" />
        </section>

        <section class="diagnostic-section">
          <header><div><strong>主机实际监听</strong><span>本次 SSH 快照中的 TCP / UDP 监听表，不包含连接态 socket。</span></div></header>
          <DataTable v-if="snapshot.actual_listeners.length" caption="主机实际 TCP 和 UDP 监听" :row-count="snapshot.actual_listeners.length" :min-width="560">
            <thead><tr><th>网络</th><th>状态</th><th>地址</th><th>端口</th></tr></thead>
            <tbody><tr v-for="listener in snapshot.actual_listeners" :key="`${listener.network}-${listener.address}-${listener.port}`"><td class="mono">{{ listener.network.toUpperCase() }}</td><td>{{ listener.state || '—' }}</td><td class="mono">{{ listener.address || '*' }}</td><td class="mono">{{ listener.port }}</td></tr></tbody>
          </DataTable>
          <EmptyState v-else icon="activity" title="没有读取到监听表" description="ss 没有返回可解析的 TCP / UDP 监听记录。" />
        </section>

        <PageAlert v-if="!snapshot.capabilities.udp_external_reachability" tone="info" title="UDP 外部可达性未做推断">
          通用 UDP 无法通过一次握手可靠判断业务可达性，因此这里仅确认本机 UDP socket 是否存在；不会把未探测状态伪装成可达或不可达。
        </PageAlert>

        <section v-if="snapshot.recent_zero_logs || snapshot.recent_kernel_logs" class="diagnostic-section">
          <header><div><strong>受限诊断日志</strong><span>只包含有限时间窗口和大小上限，后端会在返回前做敏感字段脱敏。</span></div></header>
          <OutputBlock v-if="snapshot.recent_zero_logs" :value="snapshot.recent_zero_logs" label="Zero 最近 warning / error" :max-length="65536" />
          <OutputBlock v-if="snapshot.recent_kernel_logs" :value="snapshot.recent_kernel_logs" label="内核最近 warning / error" :max-length="32768" />
        </section>
      </template>

      <EmptyState v-else-if="sshReady && !error" icon="activity" title="尚未运行诊断" description="运行一次按需诊断，确认配置 listener、主机实际监听和资源状态。">
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
import { runNodeDiagnostics, type NodeDiagnosticClassification, type NodeDiagnosticExpectedListener, type NodeDiagnosticSnapshot } from '../api/nodeDiagnostics'
import { normalizeApiErrorMessage } from '../utils/apiError'
import DataTable from './DataTable.vue'
import EmptyState from './EmptyState.vue'
import ModalDialog from './ModalDialog.vue'
import OutputBlock from './OutputBlock.vue'
import PageAlert from './PageAlert.vue'
import StatusBadge from './StatusBadge.vue'
import TimeBadge from './TimeBadge.vue'
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
    if (sequence === requestSequence) error.value = normalizeApiErrorMessage(cause, '节点运行诊断失败，请确认 SSH、Zero 服务和主机权限。')
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function classificationLabel(value: NodeDiagnosticClassification) {
  return ({ healthy: '数据面正常', data_plane_missing: '数据面缺失', network_reachability: '外部不可达', resource_pressure: '资源压力', unknown: '状态未知' } as const)[value] || '状态未知'
}

function classificationTone(value: NodeDiagnosticClassification): 'success' | 'warning' | 'danger' | 'neutral' {
  if (value === 'healthy') return 'success'
  if (value === 'data_plane_missing') return 'danger'
  if (value === 'unknown') return 'neutral'
  return 'warning'
}

function classificationIcon(value: NodeDiagnosticClassification) {
  return value === 'healthy' ? 'check' : value === 'unknown' ? 'search' : 'alert'
}

function ratioValue(current?: number, limit?: number) {
  if (!limit) return current ? String(current) : '不可用'
  return `${current || 0} / ${limit}`
}

function ratioPercent(value?: number) {
  if (!Number.isFinite(value)) return '未提供限制'
  return `使用率 ${Math.round((value || 0) * 100)}%`
}

function reachabilityTone(value: string): 'success' | 'warning' | 'neutral' {
  if (value === 'reachable') return 'success'
  if (value === 'unreachable') return 'warning'
  return 'neutral'
}

function reachabilityLabel(listener: NodeDiagnosticExpectedListener) {
  if (!listener.networks.includes('tcp')) return '不适用'
  if (listener.external_reachability === 'reachable') return '可连接'
  if (listener.external_reachability === 'unreachable') return '不可连接'
  return '未探测'
}
</script>

<style scoped>
.diagnostic-stack{display:grid;gap:14px;min-width:0}
.diagnostic-loading{min-height:160px;display:flex;align-items:center;justify-content:center;gap:12px;color:var(--muted)}
.diagnostic-loading>.ui-icon{width:26px;height:26px;color:var(--primary)}
.diagnostic-loading>div{display:grid;gap:4px}.diagnostic-loading strong{color:var(--text-strong);font-size:12px}.diagnostic-loading span{max-width:560px;font-size:10px;line-height:1.55}
.diagnostic-summary{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;padding:13px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}
.diagnostic-summary>div{display:grid;justify-items:start;gap:6px}.diagnostic-summary strong{color:var(--text-strong);font-size:12px}.diagnostic-summary span{color:var(--muted);font-size:9px}
.diagnostic-facts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px}
.diagnostic-facts>div{display:grid;gap:4px;padding:11px;border:1px solid var(--line);border-radius:9px;background:var(--surface)}
.diagnostic-facts span{color:var(--muted);font-size:9px}.diagnostic-facts strong{min-width:0;overflow:hidden;text-overflow:ellipsis;color:var(--text-strong);font-size:11px}.diagnostic-facts small{color:var(--muted);font-size:9px}
.diagnostic-section{display:grid;gap:10px}.diagnostic-section>header{display:flex;align-items:flex-start;justify-content:space-between;gap:10px;padding-bottom:8px;border-bottom:1px solid var(--line)}
.diagnostic-section>header>div{display:grid;gap:3px}.diagnostic-section>header strong{color:var(--text-strong);font-size:11px}.diagnostic-section>header span{color:var(--muted);font-size:9px;line-height:1.5}
.mono{font-family:var(--font-mono);font-variant-numeric:tabular-nums}
@media(max-width:800px){.diagnostic-facts{grid-template-columns:repeat(2,minmax(0,1fr))}.diagnostic-summary{align-items:stretch;flex-direction:column}}
@media(max-width:520px){.diagnostic-facts{grid-template-columns:1fr}}
</style>
