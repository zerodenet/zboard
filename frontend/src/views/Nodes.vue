<template>
  <section>
    <PageHeader title="节点资产" description="独立管理 VPS、运维通道、Zero 连接与计量凭证；协议服务在单独页面绑定节点。" eyebrow="Infrastructure">
      <template #actions>
        <button class="button button-secondary" type="button" :disabled="loading" @click="refresh"><UiIcon name="refresh" />刷新</button>
        <button class="button" type="button" @click="openCreate"><UiIcon name="plus" />登记 VPS</button>
      </template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="node-layout">
      <aside class="panel node-list-panel">
        <header class="panel-header"><div><h2>主机清单</h2><p>主机可以独立存在，不要求先配置协议。</p></div><span class="count-label">{{ nodes.length }}</span></header>
        <div v-if="nodes.length" class="node-list">
          <button v-for="node in nodes" :key="node.id" type="button" :class="{ active: selectedNode?.id === node.id }" @click="selectedNode = node">
            <span class="node-state" :class="{ online: node.connector_online, disabled: !node.is_enabled }"></span>
            <div><strong>{{ node.name }}</strong><p>{{ node.region || '未设置区域' }} · {{ node.address || '未设置业务地址' }}</p><small>{{ lifecycleLabel(node.lifecycle_status) }}</small></div>
            <UiIcon name="chevron" />
          </button>
        </div>
        <EmptyState v-else icon="nodes" title="还没有 VPS" description="先登记主机资产，协议和套餐可以稍后配置。" />
      </aside>

      <main v-if="selectedNode" class="stack node-detail">
        <article class="panel node-summary">
          <div class="node-summary-main">
            <span class="node-avatar"><UiIcon name="nodes" /></span>
            <div><div class="title-line"><h2>{{ selectedNode.name }}</h2><StatusBadge :tone="lifecycleTone(selectedNode)">{{ lifecycleLabel(selectedNode.lifecycle_status) }}</StatusBadge></div><p>{{ selectedNode.region || '未设置区域' }} · <span class="mono">{{ selectedNode.address || '未设置默认业务地址' }}</span></p><small>{{ selectedNode.remark || '暂无备注' }}</small></div>
          </div>
          <div class="node-actions">
            <button class="button button-secondary button-sm" type="button" @click="openEdit(selectedNode)"><UiIcon name="edit" />编辑资产</button>
            <button class="button button-secondary button-sm" type="button" @click="openSSH(selectedNode)"><UiIcon name="key" />SSH 连接</button>
            <button class="button button-secondary button-sm" type="button" :disabled="testingNode === selectedNode.id" @click="testSSH(selectedNode.id)"><UiIcon name="activity" />{{ testingNode === selectedNode.id ? '验证中…' : '验证 SSH' }}</button>
            <button class="button button-secondary button-sm" type="button" :disabled="!selectedNode.ssh_host || !selectedNode.ssh_user" @click="openTerminal(selectedNode)"><UiIcon name="terminal" />打开终端</button>
            <button v-if="selectedNode.ssh_host_key_fingerprint" class="button button-danger button-sm" type="button" @click="resetSSHHostKey(selectedNode)">重新信任主机</button>
          </div>
        </article>

        <div class="readiness-grid">
          <article><span><UiIcon name="activity" /></span><div><strong>Zero 主动连接</strong><p>{{ selectedNode.connector_last_seen_at ? `最近心跳 ${formatDateTime(selectedNode.connector_last_seen_at)}` : '尚未连接' }}</p></div><StatusBadge :tone="selectedNode.connector_online ? 'success' : 'warning'">{{ selectedNode.connector_online ? '在线' : '离线' }}</StatusBadge></article>
          <article><span><UiIcon name="key" /></span><div><strong>SSH 运维通道</strong><p>{{ sshDescription(selectedNode) }}</p></div><StatusBadge :tone="selectedNode.ssh_verified_at ? 'success' : 'warning'">{{ selectedNode.ssh_verified_at ? '已验证' : '待验证' }}</StatusBadge></article>
          <article><span><UiIcon name="shield" /></span><div><strong>可信计量</strong><p>{{ selectedNode.traffic_secret_prefix ? `凭证 ${selectedNode.traffic_secret_prefix}…` : '尚未创建上报凭证' }}</p></div><StatusBadge :tone="selectedNode.traffic_secret_prefix && !selectedNode.traffic_secret_revoked_at ? 'success' : 'warning'">{{ selectedNode.traffic_secret_prefix && !selectedNode.traffic_secret_revoked_at ? '已配置' : '未配置' }}</StatusBadge></article>
          <article><span><UiIcon name="nodes" /></span><div><strong>资产状态</strong><p>{{ selectedNode.is_enabled ? '允许承载对外服务' : '已从交付路径停用' }}</p></div><StatusBadge :tone="selectedNode.is_enabled ? 'success' : 'neutral'">{{ selectedNode.is_enabled ? '启用' : '停用' }}</StatusBadge></article>
          <article><span><UiIcon name="activity" /></span><div><strong>Zero 内核</strong><p>{{ kernelDescription }}</p></div><StatusBadge :tone="kernelTone">{{ kernelStatusLabel }}</StatusBadge></article>
        </div>

        <article class="panel kernel-panel">
          <header class="panel-header">
            <div><h2>Zero 内核自动化</h2><p>检测真实平台与服务状态，锁定匹配节点的受信任稳定制品后执行安装、升级或配置对齐；任一验收失败都会恢复上一版。</p></div>
            <StatusBadge :tone="kernelTone">{{ kernelStatusLabel }}</StatusBadge>
          </header>
          <div class="panel-body kernel-body">
            <div class="kernel-facts">
              <div><span>已安装版本</span><strong>{{ kernelState?.installed_version || '未检测' }}</strong></div>
              <div><span>最新稳定版</span><strong>{{ latestRelease?.tag || (releaseLoading ? '查询中…' : '未查询') }}</strong></div>
              <div><span>平台</span><strong>{{ [kernelState?.platform_os, kernelState?.architecture].filter(Boolean).join(' · ') || '未检测' }}</strong></div>
              <div><span>运行库</span><strong>{{ kernelState?.libc || '未检测' }}</strong></div>
              <div><span>systemd / 进程</span><strong>{{ serviceLabel(kernelState?.service_status) }}</strong></div>
              <div><span>控制通道</span><strong>{{ controlLabel(kernelState?.control_status) }}</strong></div>
              <div><span>最后检测</span><strong>{{ kernelState?.last_detected_at ? formatDateTime(kernelState.last_detected_at) : '从未' }}</strong></div>
              <div><span>建议动作</span><strong>{{ kernelActionLabel(kernelState?.recommended_action) }}</strong></div>
            </div>
            <p v-if="kernelState?.last_error" class="kernel-error"><UiIcon name="alert" />{{ kernelState.last_error }}</p>
            <div class="kernel-actions">
              <button class="button button-secondary button-sm" type="button" :disabled="Boolean(kernelBusy) || !selectedNode.ssh_verified_at" @click="detectKernel"><UiIcon name="search" />{{ kernelBusy === 'detect' ? '检测中…' : '检测内核' }}</button>
              <button class="button button-sm" type="button" :disabled="Boolean(kernelBusy) || !selectedNode.ssh_verified_at || releaseLoading || kernelState?.status === 'unsupported'" @click="reconcileKernel"><UiIcon name="play" />{{ kernelBusy === 'reconcile' ? kernelPhaseLabel(kernelState?.phase) : reconcileButtonLabel }}</button>
              <small v-if="!selectedNode.ssh_verified_at">请先完成 SSH 验证，自动化不会绕过运维通道校验。</small>
              <small v-else>现代 glibc 节点使用官方 GNU 制品，旧版 Linux 使用面板托管并校验 SHA-256 的 musl 制品；不会升级系统 libc，也不会自动降级内核。</small>
            </div>
            <div v-if="kernelOperations.length" class="kernel-history">
              <div v-for="operation in kernelOperations.slice(0, 5)" :key="operation.id">
                <span class="operation-dot" :class="operation.status"></span>
                <div><strong>{{ operationLabel(operation.operation_type) }} · #{{ operation.id }}</strong><p>{{ operation.result_summary || operation.error || kernelPhaseLabel(operation.phase) }}</p></div>
                <time>{{ formatDateTime(operation.created_at) }}</time>
              </div>
            </div>
          </div>
        </article>

        <div class="section-grid">
          <article class="panel span-6"><header class="panel-header"><div><h2>Zero 连接凭证</h2><p>用于主机主动向面板发送心跳和领取命令。</p></div></header><div class="panel-body action-card"><p v-if="selectedNode.node_credential_prefix">当前前缀 <code>{{ selectedNode.node_credential_prefix }}…</code></p><p v-else>尚未生成。</p><div><button class="button button-secondary button-sm" type="button" @click="rotateConnector(selectedNode)">{{ selectedNode.node_credential_prefix ? '轮换' : '生成' }}</button><button v-if="selectedNode.node_credential_prefix && !selectedNode.node_credential_revoked_at" class="button button-danger button-sm" type="button" @click="revokeConnector(selectedNode)">吊销</button></div></div></article>
          <article class="panel span-6"><header class="panel-header"><div><h2>流量上报凭证</h2><p>与 SSH 和 Zero 连接凭证彼此独立。</p></div></header><div class="panel-body action-card"><p v-if="selectedNode.traffic_secret_prefix">当前前缀 <code>{{ selectedNode.traffic_secret_prefix }}…</code></p><p v-else>尚未生成。</p><div><button class="button button-secondary button-sm" type="button" @click="rotateReport(selectedNode)">{{ selectedNode.traffic_secret_prefix ? '轮换' : '生成' }}</button><button v-if="selectedNode.traffic_secret_prefix && !selectedNode.traffic_secret_revoked_at" class="button button-danger button-sm" type="button" @click="revokeReport(selectedNode)">吊销</button></div></div></article>
        </div>
      </main>

      <article v-else class="panel node-empty"><EmptyState icon="nodes" title="选择一台 VPS" description="查看和维护主机资产状态。" /></article>
    </div>

    <ModalDialog :open="createOpen" title="登记 VPS" description="这里只建立主机资产，不会自动创建或部署协议。" :busy="saving" @close="createOpen = false">
      <form id="node-create-form" class="form-grid" @submit.prevent="create">
        <label class="field"><span>主机名称</span><input v-model.trim="createForm.name" required placeholder="香港 VPS 01" /></label>
        <label class="field"><span>区域</span><input v-model.trim="createForm.region" placeholder="Hong Kong" /></label>
        <label class="field field-full"><span>默认业务地址</span><input v-model.trim="createForm.address" placeholder="可选，用于新建协议时预填" /></label>
        <label class="field field-full"><span>备注</span><textarea v-model.trim="createForm.remark" rows="3" placeholder="供应商、机房、用途、到期时间等"></textarea></label>
      </form>
      <template #footer><button class="button button-secondary" type="button" @click="createOpen = false">取消</button><button class="button" form="node-create-form" type="submit" :disabled="saving">保存 VPS</button></template>
    </ModalDialog>

    <ModalDialog :open="editOpen" title="编辑 VPS" description="维护资产元数据和生命周期；维护或退役会自动停止对外交付。" :busy="saving" @close="editOpen = false">
      <form id="node-edit-form" class="form-grid" @submit.prevent="saveNode">
        <label class="field"><span>主机名称</span><input v-model.trim="editForm.name" required /></label>
        <label class="field"><span>区域</span><input v-model.trim="editForm.region" /></label>
        <label class="field field-full"><span>默认业务地址</span><input v-model.trim="editForm.address" /></label>
        <label class="field"><span>生命周期</span><select v-model="editForm.lifecycle_status"><option value="active">正常</option><option value="maintenance">维护</option><option value="retired">退役</option></select></label>
        <label class="check-field"><input v-model="editForm.is_enabled" type="checkbox" :disabled="editForm.lifecycle_status !== 'active'" /><span>允许承载对外服务</span></label>
        <label class="field field-full"><span>备注</span><textarea v-model.trim="editForm.remark" rows="3"></textarea></label>
      </form>
      <template #footer><button class="button button-secondary" type="button" @click="editOpen = false">取消</button><button class="button" form="node-edit-form" type="submit" :disabled="saving">保存</button></template>
    </ModalDialog>

    <ModalDialog :open="sshOpen" title="SSH 与系统权限" description="登录凭证和系统提权分开管理；只有安装、systemd 与协议配置等系统操作会提权。" size="lg" :busy="saving" @close="sshOpen = false">
      <form id="ssh-form" class="form-grid" @submit.prevent="saveSSH">
        <label class="field"><span>SSH 主机</span><input v-model.trim="sshForm.ssh_host" required placeholder="192.0.2.10" /></label>
        <label class="field"><span>端口</span><input v-model.number="sshForm.ssh_port" type="number" min="1" max="65535" required /></label>
        <label class="field"><span>用户</span><input v-model.trim="sshForm.ssh_user" required /></label>
        <label class="field"><span>认证方式</span><select v-model="sshForm.ssh_auth_method"><option value="password">密码</option><option value="private_key">私钥</option></select></label>
        <label v-if="sshForm.ssh_auth_method === 'password'" class="field field-full"><span>{{ sshForm.hasCredential ? '替换密码（可留空）' : 'SSH 密码' }}</span><input v-model="sshForm.ssh_password" type="password" autocomplete="new-password" :required="!sshForm.hasCredential" /></label>
        <template v-else><label class="field field-full"><span>{{ sshForm.hasCredential ? '替换私钥（可留空）' : 'SSH 私钥' }}</span><textarea v-model="sshForm.ssh_private_key" rows="8" spellcheck="false" :required="!sshForm.hasCredential" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></textarea></label><label class="field field-full"><span>私钥口令（可选）</span><input v-model="sshForm.ssh_private_key_passphrase" type="password" autocomplete="new-password" /><small class="field-hint">留空表示保留已保存口令；替换私钥时可同时填写新口令。</small></label><label v-if="sshForm.hasCredential" class="check-field field-full"><input v-model="sshForm.clearPassphrase" type="checkbox" /><span>清除已保存的私钥口令</span></label></template>
        <label class="field"><span>系统提权</span><select v-model="sshForm.ssh_privilege_mode"><option value="none">直接登录 root</option><option value="sudo">sudo 提权</option><option value="su">su 切换 root</option></select></label>
        <label v-if="sshForm.ssh_privilege_mode !== 'none'" class="field"><span>{{ sshForm.hasPrivilegePassword ? '替换提权密码（可留空）' : sshForm.ssh_privilege_mode === 'su' ? 'root 密码' : 'sudo 密码（可选）' }}</span><input v-model="sshForm.ssh_privilege_password" type="password" autocomplete="new-password" :required="sshForm.ssh_privilege_mode === 'su' && !sshForm.hasPrivilegePassword" /></label>
        <label v-if="sshForm.ssh_privilege_mode === 'sudo'" class="check-field field-full"><input v-model="sshForm.passwordlessSudo" type="checkbox" /><span>该用户已配置免密 sudo（启用后会清除已保存的提权密码）</span></label>
        <p v-if="sshForm.ssh_privilege_mode === 'su'" class="field-hint field-full">适用于可使用 <code>su root -c</code> 的主机；root 密码独立加密保存，不会写入命令、任务输出或审计日志。</p>
        <p class="field-hint field-full">首次验证会自动固定服务器身份；以后发现主机密钥变化时会停止连接。仅在确认 VPS 已重装或更换后，使用“重新信任主机”。</p>
      </form>
      <template #footer><button class="button button-secondary" type="button" @click="sshOpen = false">取消</button><button class="button" form="ssh-form" type="submit" :disabled="saving">保存连接配置</button></template>
    </ModalDialog>

    <ModalDialog :open="Boolean(secretModal.value)" :title="secretModal.title" description="完整凭证只显示一次，请立即复制并安全保存。" @close="secretModal.value = ''">
      <div class="secret-card"><textarea :value="secretModal.value" rows="8" readonly></textarea><button class="button button-secondary" type="button" @click="copySecret"><UiIcon name="copy" />{{ secretModal.copied ? '已复制' : '复制' }}</button></div>
    </ModalDialog>

    <SshTerminalDialog :open="terminalOpen" :node="terminalNode" @close="terminalOpen = false" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { createNode, detectNodeKernel, fetchLatestZeroRelease, fetchNodeKernel, fetchNodes, reconcileNodeKernel, resetNodeSSHHostKey, revokeNodeConnectorCredential, revokeNodeReportCredential, rotateNodeConnectorCredential, rotateNodeReportCredential, testNodeSSH, updateNode, updateNodeSSH, type NodeKernelOperation, type NodeKernelState, type ZeroRelease } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import SshTerminalDialog from '../components/SshTerminalDialog.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { confirmAction, notify } from '../utils/feedback'
import { formatDateTime } from '../utils/format'

const nodes = ref<any[]>([])
const selectedNode = ref<any>(null)
const loading = ref(false), saving = ref(false), testingNode = ref(0)
const error = ref(''), message = ref('')
const createOpen = ref(false), editOpen = ref(false), sshOpen = ref(false)
const terminalOpen = ref(false), terminalNode = ref<any>(null)
const kernelState = ref<NodeKernelState | null>(null), kernelOperations = ref<NodeKernelOperation[]>([])
const latestRelease = ref<ZeroRelease | null>(null), releaseLoading = ref(false)
const kernelBusy = ref<'' | 'detect' | 'reconcile'>('')
let kernelPollTimer: number | undefined
const createForm = reactive({ name: '', region: '', address: '', remark: '' })
const editForm = reactive({ id: 0, name: '', region: '', address: '', remark: '', lifecycle_status: 'active', is_enabled: true })
const sshForm = reactive({ node_id: 0, ssh_host: '', ssh_port: 22, ssh_user: 'root', ssh_auth_method: 'password' as 'password' | 'private_key', ssh_password: '', ssh_private_key: '', ssh_private_key_passphrase: '', clearPassphrase: false, hasCredential: false, ssh_privilege_mode: 'none' as 'none' | 'sudo' | 'su', ssh_privilege_password: '', hasPrivilegePassword: false, passwordlessSudo: false })
const secretModal = reactive({ title: '', value: '', copied: false })

const kernelTone = computed<'success' | 'warning' | 'neutral'>(() => kernelState.value?.status === 'healthy' ? 'success' : kernelState.value?.status === 'unknown' ? 'neutral' : 'warning')
const kernelStatusLabel = computed(() => ({ unknown: '未检测', not_installed: '未安装', healthy: '健康', degraded: '异常', failed: '操作失败', unsupported: '制品不兼容' } as Record<string, string>)[kernelState.value?.status || 'unknown'] || kernelState.value?.status)
const kernelDescription = computed(() => kernelState.value?.installed_version ? `Zero ${kernelState.value.installed_version} · ${serviceLabel(kernelState.value.service_status)}` : kernelStatusLabel.value)
const reconcileButtonLabel = computed(() => kernelState.value?.status === 'unsupported' ? '当前制品不兼容' : kernelState.value?.status === 'not_installed' ? '自动安装' : kernelState.value?.recommended_action === 'configure' ? '应用配置' : '安装 / 升级 / 修复')

function lifecycleLabel(value?: string) { return ({ active: '正常', maintenance: '维护', retired: '退役' } as Record<string, string>)[value || 'active'] || value }
function lifecycleTone(node: any): 'success' | 'warning' | 'neutral' { return node.lifecycle_status === 'maintenance' ? 'warning' : node.lifecycle_status === 'retired' ? 'neutral' : node.is_enabled ? 'success' : 'neutral' }
function sshDescription(node: any) { if (!node.ssh_host) return '尚未配置'; const auth = node.ssh_auth_method === 'private_key' ? '私钥' : '密码'; const privilege = ({ none: 'root 直连', sudo: 'sudo 提权', su: 'su 提权' } as Record<string,string>)[node.ssh_privilege_mode || 'none']; const host = node.ssh_host_key_fingerprint ? '主机身份已固定' : '等待首次连接'; return `${auth}认证 · ${privilege} · ${host}` }
function serviceLabel(value?: string) { return ({ active: '运行中', inactive: '已停止', failed: '启动失败', not_found: '未安装', unknown: '未知' } as Record<string, string>)[value || 'unknown'] || value }
function controlLabel(value?: string) { return value === 'healthy' ? '健康' : value === 'unavailable' ? '不可用' : '未知' }
function kernelActionLabel(value?: string) { return ({ detect: '先检测', install: '安装', upgrade: '升级', repair: '修复', configure: '同步配置', check_release: '检查新版本', manual_review: '人工确认', none: '无需操作' } as Record<string, string>)[value || 'detect'] || value }
function operationLabel(value?: string) { return ({ detect: '环境检测', reconcile: '状态对齐', install: '安装', upgrade: '升级', repair: '修复', configure: '配置同步', none: '状态确认' } as Record<string, string>)[value || 'reconcile'] || value }
function kernelPhaseLabel(value?: string) { return ({ queued: '排队中…', detecting: '检测中…', resolving_release: '匹配制品…', downloading: '校验制品…', staging: '暂存并切换…', verifying: '本地健康检查…', waiting_heartbeat: '等待面板心跳…', completed: '已完成' } as Record<string, string>)[value || 'queued'] || '处理中…' }

async function loadKernel(nodeID?: number) {
  if (!nodeID) { kernelState.value = null; kernelOperations.value = []; return }
  try { const result = await fetchNodeKernel(nodeID); kernelState.value = result.state; kernelOperations.value = result.operations || [] } catch (e: any) { error.value = e?.response?.data?.message || '内核状态加载失败。' }
}
async function loadLatestRelease() { releaseLoading.value = true; try { latestRelease.value = await fetchLatestZeroRelease() } catch (e: any) { error.value = e?.response?.data?.message || 'Zero 稳定版本查询失败。' } finally { releaseLoading.value = false } }
async function detectKernel() { if (!selectedNode.value) return; kernelBusy.value = 'detect'; error.value = ''; message.value = ''; try { const result = await detectNodeKernel(selectedNode.value.id); kernelState.value = result.state; message.value = 'Zero 内核检测完成，页面显示的是服务器真实状态。'; await loadKernel(selectedNode.value.id) } catch (e: any) { error.value = e?.response?.data?.message || 'Zero 内核检测失败。'; await loadKernel(selectedNode.value.id) } finally { kernelBusy.value = '' } }
function stopKernelPolling() { if (kernelPollTimer !== undefined) { window.clearInterval(kernelPollTimer); kernelPollTimer = undefined } }
function startKernelPolling(nodeID: number) {
  stopKernelPolling()
  kernelPollTimer = window.setInterval(async () => {
    if (kernelBusy.value !== 'reconcile' || selectedNode.value?.id !== nodeID) return
    try {
      const result = await fetchNodeKernel(nodeID)
      if (kernelBusy.value === 'reconcile' && selectedNode.value?.id === nodeID) {
        kernelState.value = result.state
        kernelOperations.value = result.operations || []
      }
    } catch { /* The reconcile request remains the source of the final error. */ }
  }, 1000)
}
async function reconcileKernel() { if (!selectedNode.value) return; const nodeID = selectedNode.value.id; const target = latestRelease.value?.tag || '最新稳定版'; if (!await confirmAction({ title: '对齐 Zero 内核', message: `将把这台 VPS 的 Zero 对齐到 ${target} 及当前启用协议配置。操作会校验制品、systemd、控制通道和面板心跳，失败自动回滚。`, confirmText: '开始对齐' })) return; kernelBusy.value = 'reconcile'; error.value = ''; message.value = ''; startKernelPolling(nodeID); try { const result = await reconcileNodeKernel(nodeID); message.value = result.changed ? `Zero 已完成${operationLabel(result.action)}并通过本地与面板健康验收。` : 'Zero 二进制与配置已是期望状态，无需变更。'; notify('内核操作已完成', message.value, 'success'); await Promise.all([refresh(), loadKernel(nodeID), loadLatestRelease()]) } catch (e: any) { error.value = e?.response?.data?.message || 'Zero 自动化操作失败；请查看操作记录中的阶段与错误。'; await loadKernel(nodeID) } finally { stopKernelPolling(); kernelBusy.value = '' } }

async function refresh() { loading.value = true; error.value = ''; try { const data = await fetchNodes(); nodes.value = data; selectedNode.value = selectedNode.value ? data.find((item: any) => item.id === selectedNode.value.id) || data[0] || null : data[0] || null } catch (e: any) { error.value = e?.response?.data?.message || '节点加载失败。' } finally { loading.value = false } }
function openCreate() { createOpen.value = true }
async function create() { saving.value = true; error.value = ''; try { const node = await createNode({ ...createForm }); Object.assign(createForm, { name: '', region: '', address: '', remark: '' }); createOpen.value = false; await refresh(); selectedNode.value = nodes.value.find(item => item.id === node.id) || node; message.value = 'VPS 已登记；协议服务可在协议页面单独创建。' } catch (e: any) { error.value = e?.response?.data?.message || '节点登记失败。' } finally { saving.value = false } }
function openEdit(node: any) { Object.assign(editForm, { id: node.id, name: node.name, region: node.region || '', address: node.address || '', remark: node.remark || '', lifecycle_status: node.lifecycle_status || 'active', is_enabled: Boolean(node.is_enabled) }); editOpen.value = true }
async function saveNode() { saving.value = true; error.value = ''; try { await updateNode(editForm.id, { name: editForm.name, region: editForm.region, address: editForm.address, remark: editForm.remark, lifecycle_status: editForm.lifecycle_status, is_enabled: editForm.lifecycle_status === 'active' && editForm.is_enabled }); editOpen.value = false; message.value = '节点资产已更新。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '节点更新失败。' } finally { saving.value = false } }
function openSSH(node: any) { const privilegeMode = node.ssh_privilege_mode || 'none'; Object.assign(sshForm, { node_id: node.id, ssh_host: node.ssh_host || '', ssh_port: node.ssh_port || 22, ssh_user: node.ssh_user || 'root', ssh_auth_method: node.ssh_auth_method || 'password', ssh_password: '', ssh_private_key: '', ssh_private_key_passphrase: '', clearPassphrase: false, hasCredential: Boolean(node.ssh_host && node.ssh_user), ssh_privilege_mode: privilegeMode, ssh_privilege_password: '', hasPrivilegePassword: Boolean(node.ssh_privilege_password_configured), passwordlessSudo: privilegeMode === 'sudo' && !node.ssh_privilege_password_configured }); sshOpen.value = true }
function openTerminal(node: any) { terminalNode.value = node; terminalOpen.value = true }
async function saveSSH() { saving.value = true; error.value = ''; try { const payload: any = { ssh_host: sshForm.ssh_host, ssh_port: sshForm.ssh_port, ssh_user: sshForm.ssh_user, ssh_auth_method: sshForm.ssh_auth_method, ssh_privilege_mode: sshForm.ssh_privilege_mode }; if (sshForm.ssh_auth_method === 'password' && sshForm.ssh_password) payload.ssh_password = sshForm.ssh_password; if (sshForm.ssh_auth_method === 'private_key' && sshForm.ssh_private_key) payload.ssh_private_key = sshForm.ssh_private_key; if (sshForm.ssh_auth_method === 'private_key' && (sshForm.ssh_private_key_passphrase || sshForm.clearPassphrase)) payload.ssh_private_key_passphrase = sshForm.clearPassphrase ? '' : sshForm.ssh_private_key_passphrase; if (sshForm.ssh_privilege_mode === 'none' || (sshForm.ssh_privilege_mode === 'sudo' && sshForm.passwordlessSudo)) payload.ssh_privilege_password = ''; else if (sshForm.ssh_privilege_password) payload.ssh_privilege_password = sshForm.ssh_privilege_password; await updateNodeSSH(sshForm.node_id, payload); sshOpen.value = false; message.value = 'SSH 与提权配置已保存，请执行连接验证。'; notify('连接配置已保存', '验证 SSH 时会同时检查已配置的系统提权能力。', 'success'); await refresh() } catch (e: any) { error.value = e?.response?.data?.message || 'SSH 配置保存失败。' } finally { saving.value = false } }
async function testSSH(id: number) { testingNode.value = id; error.value = ''; try { const result = await testNodeSSH(id); message.value = `SSH 验证成功，耗时 ${result.latency_ms || 0}ms；主机身份已自动校验。`; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || 'SSH 验证失败。' } finally { testingNode.value = 0 } }
async function resetSSHHostKey(node: any) { if (!await confirmAction({ title: '重新信任主机', message: '仅当 VPS 已重装或主机密钥已确认更换时继续。重置后，下一次 SSH 连接会自动登记新的主机身份。', confirmText: '清除旧身份', tone: 'danger' })) return; error.value = ''; try { await resetNodeSSHHostKey(node.id); message.value = '已清除旧主机身份；下一次 SSH 连接将自动重新登记。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '重新信任主机失败。' } }
async function rotateConnector(node: any) { if (node.node_credential_prefix && !await confirmAction({ title: '轮换 Zero 凭证', message: '轮换后旧 Zero 连接凭证会立即失效，节点需要应用新配置。', confirmText: '确认轮换', tone: 'danger' })) return; try { const result = await rotateNodeConnectorCredential(node.id); const config = JSON.stringify({ push: { url: window.location.origin, node_id: result.node_id, api_key: result.api_key, heartbeat_interval_seconds: 30, pull_commands: true, command_poll_interval_seconds: 10 } }, null, 2); Object.assign(secretModal, { title: 'Zero 主动连接配置', value: config, copied: false }); await refresh() } catch (e: any) { error.value = e?.response?.data?.message || 'Zero 连接凭证操作失败。' } }
async function revokeConnector(node: any) { if (!await confirmAction({ title: '吊销 Zero 凭证', message: '吊销后节点将无法继续向面板发送心跳或领取命令。', confirmText: '确认吊销', tone: 'danger' })) return; try { await revokeNodeConnectorCredential(node.id); message.value = 'Zero 连接凭证已吊销。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '吊销失败。' } }
async function rotateReport(node: any) { if (node.traffic_secret_prefix && !await confirmAction({ title: '轮换流量凭证', message: '轮换后旧流量上报凭证会立即失效。', confirmText: '确认轮换', tone: 'danger' })) return; try { const result = await rotateNodeReportCredential(node.id); Object.assign(secretModal, { title: '流量上报凭证', value: result.secret, copied: false }); await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '流量上报凭证操作失败。' } }
async function revokeReport(node: any) { if (!await confirmAction({ title: '吊销流量凭证', message: '吊销后节点将无法继续提交可信流量记录。', confirmText: '确认吊销', tone: 'danger' })) return; try { await revokeNodeReportCredential(node.id); message.value = '流量上报凭证已吊销。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '吊销失败。' } }
async function copySecret() { try { await navigator.clipboard.writeText(secretModal.value); secretModal.copied = true } catch { secretModal.copied = false } }

watch(() => selectedNode.value?.id, id => void loadKernel(id))
onMounted(async () => { await Promise.all([refresh(), loadLatestRelease()]) })
onBeforeUnmount(stopKernelPolling)
</script>

<style scoped>
.page-alert{margin-bottom:14px}.count-label{color:var(--muted);font-size:11px}.node-layout{display:grid;grid-template-columns:300px minmax(0,1fr);align-items:start;gap:16px}.node-list-panel{position:sticky;top:82px;overflow:hidden}.node-list{display:grid}.node-list>button{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:15px 16px;text-align:left;border:0;border-bottom:1px solid var(--line);background:#fff}.node-list>button:hover,.node-list>button.active{background:#f7faff}.node-list>button.active{box-shadow:inset 3px 0 var(--primary)}.node-state{width:9px;height:9px;border-radius:50%;background:#f79009;box-shadow:0 0 0 4px #fffaeb}.node-state.online{background:#12b76a;box-shadow:0 0 0 4px #ecfdf3}.node-state.disabled{background:#98a2b3;box-shadow:0 0 0 4px #f2f4f7}.node-list strong{font-size:12px}.node-list p,.node-list small{margin:3px 0 0;color:var(--muted);font-size:9px}.node-list small{color:var(--primary)}.node-detail{min-width:0}.node-summary{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:20px}.node-summary-main{display:flex;align-items:center;gap:14px}.node-avatar{width:46px;height:46px;display:grid;place-items:center;border-radius:12px;color:var(--primary);background:var(--primary-soft);font-size:21px}.title-line{display:flex;align-items:center;gap:10px}.title-line h2{margin:0;font-size:19px}.node-summary-main p{margin:5px 0 2px;color:var(--muted);font-size:11px}.node-summary-main small{color:var(--subtle);font-size:9px}.node-actions{display:flex;flex-wrap:wrap;gap:7px}.readiness-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.readiness-grid article{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:11px;padding:15px;border:1px solid var(--line);border-radius:var(--radius-md);background:#fff}.readiness-grid article>span{width:34px;height:34px;display:grid;place-items:center;border-radius:9px;color:var(--primary);background:var(--primary-soft)}.readiness-grid strong{font-size:11px}.readiness-grid p{margin:3px 0 0;color:var(--muted);font-size:9px}.action-card{display:grid;gap:14px}.action-card p{margin:0;color:var(--muted);font-size:11px}.action-card>div{display:flex;gap:7px}.node-empty{min-height:420px;display:grid;place-items:center}.secret-card{display:grid;gap:14px}.secret-card textarea{font-family:Consolas,monospace;font-size:11px}.secret-card .button{justify-self:start}@media(max-width:1100px){.node-layout{grid-template-columns:1fr}.node-list-panel{position:static}.node-list{grid-template-columns:repeat(2,1fr)}}@media(max-width:720px){.node-list,.readiness-grid{grid-template-columns:1fr}.node-summary{align-items:flex-start;flex-direction:column}.node-actions{display:grid;width:100%}}
.kernel-panel{overflow:hidden}.kernel-body{display:grid;gap:16px}.kernel-facts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1px;overflow:hidden;border:1px solid var(--line);border-radius:10px;background:var(--line)}.kernel-facts>div{display:grid;gap:5px;padding:13px;background:var(--surface)}.kernel-facts span{color:var(--muted);font-size:9px}.kernel-facts strong{font-size:11px;overflow-wrap:anywhere}.kernel-error{display:flex;align-items:flex-start;gap:8px;margin:0;padding:11px;border-radius:8px;color:var(--danger);background:var(--danger-soft);font-size:10px;overflow-wrap:anywhere}.kernel-actions{display:flex;align-items:center;flex-wrap:wrap;gap:8px}.kernel-actions small{color:var(--muted);font-size:9px}.kernel-history{display:grid;border-top:1px solid var(--line)}.kernel-history>div{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:10px 2px}.kernel-history>div+div{border-top:1px solid var(--line)}.kernel-history strong{font-size:10px}.kernel-history p{margin:3px 0 0;color:var(--muted);font-size:9px;overflow-wrap:anywhere}.kernel-history time{color:var(--subtle);font-size:9px}.operation-dot{width:8px;height:8px;border-radius:50%;background:var(--warning)}.operation-dot.succeeded{background:var(--success)}.operation-dot.failed{background:var(--danger)}@media(max-width:720px){.kernel-facts{grid-template-columns:repeat(2,minmax(0,1fr))}.kernel-history>div{grid-template-columns:auto minmax(0,1fr)}.kernel-history time{grid-column:2}}
</style>
