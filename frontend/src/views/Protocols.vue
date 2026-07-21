<template>
  <section>
    <PageHeader title="协议服务" description="协议端点绑定一台 VPS，保存配置与实际部署分开；倍率只在协议端点定义。" eyebrow="Infrastructure">
      <template #actions>
        <button class="button button-secondary" type="button" :disabled="loading" @click="refresh"><UiIcon name="refresh" />刷新</button>
        <button class="button" type="button" :disabled="!nodes.length" @click="openCreate"><UiIcon name="plus" />创建协议端点</button>
      </template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="metric-grid protocol-metrics">
      <article class="metric-card"><div class="metric-top"><span class="metric-icon"><UiIcon name="activity" /></span><span class="metric-label">协议端点</span></div><strong class="metric-value">{{ endpoints.length }}</strong><span class="metric-meta">协议与节点明确关联</span></article>
      <article class="metric-card"><div class="metric-top"><span class="metric-icon success"><UiIcon name="check" /></span><span class="metric-label">已启用</span></div><strong class="metric-value">{{ activeCount }}</strong><span class="metric-meta">可加入节点组</span></article>
      <article class="metric-card"><div class="metric-top"><span class="metric-icon"><UiIcon name="nodes" /></span><span class="metric-label">承载 VPS</span></div><strong class="metric-value">{{ usedNodeCount }}</strong><span class="metric-meta">节点仍独立管理</span></article>
      <article class="metric-card"><div class="metric-top"><span class="metric-icon warning"><UiIcon name="play" /></span><span class="metric-label">最近部署失败</span></div><strong class="metric-value">{{ failedDeploymentCount }}</strong><span class="metric-meta">保存不会触发部署</span></article>
    </div>

    <div v-if="endpoints.length" class="protocol-grid">
      <article v-for="endpoint in endpoints" :key="endpoint.id" class="panel protocol-card">
        <header class="protocol-header">
          <div class="protocol-title"><span class="protocol-icon"><UiIcon name="activity" /></span><div><div class="title-line"><h2>{{ endpoint.name }}</h2><StatusBadge :tone="endpoint.is_active ? 'success' : 'neutral'">{{ endpoint.is_active ? '启用' : '停用' }}</StatusBadge></div><p>{{ endpoint.protocol }}://{{ endpoint.address }}:{{ endpoint.public_port || endpoint.port }}</p></div></div>
          <strong class="multiplier">{{ formatMultiplier(endpoint.multiplier_milli) }}</strong>
        </header>
        <dl class="protocol-meta">
          <div><dt>承载节点</dt><dd>{{ nodeName(endpoint.node_id) }}</dd></div>
          <div><dt>监听端口</dt><dd>{{ endpoint.port }}</dd></div>
          <div><dt>最近部署</dt><dd><StatusBadge :tone="deploymentTone(latestDeployment(endpoint.id)?.status)">{{ deploymentLabel(latestDeployment(endpoint.id)?.status) }}</StatusBadge></dd></div>
        </dl>
        <div v-if="latestDeployment(endpoint.id)?.error" class="deployment-error">{{ latestDeployment(endpoint.id)?.error }}</div>
        <footer class="protocol-actions">
          <button class="button button-secondary button-sm" type="button" @click="openEdit(endpoint)"><UiIcon name="edit" />编辑配置</button>
          <button class="button button-sm" type="button" :disabled="deployingID === endpoint.id" @click="deploy(endpoint)"><UiIcon name="play" />{{ deployingID === endpoint.id ? '暂存中…' : '暂存到 VPS' }}</button>
        </footer>
      </article>
    </div>
    <article v-else class="panel"><EmptyState icon="activity" title="还没有协议端点" description="先登记 VPS，再创建协议端点；保存配置不会自动连接服务器。"><template #actions><button class="button" type="button" :disabled="!nodes.length" @click="openCreate"><UiIcon name="plus" />创建协议端点</button></template></EmptyState></article>

    <ModalDialog :open="editorOpen" :title="form.id ? '编辑协议端点' : '创建协议端点'" description="协议端点是节点组的基础元素；服务端配置会加密保存，部署需要节点 SSH 已配置并验证。" size="xl" :busy="saving" @close="editorOpen = false">
      <form id="protocol-form" class="stack" @submit.prevent="save">
        <div class="form-grid form-grid-3">
          <label class="field"><span>承载 VPS</span><select v-model.number="form.node_id" required :disabled="Boolean(form.id)"><option disabled :value="0">请选择 VPS</option><option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}{{ node.is_enabled ? '' : '（已停用）' }}</option></select><small class="field-hint">创建后不可迁移；迁移请新建端点并调整节点组。</small></label>
          <label class="field"><span>端点名称</span><input v-model.trim="form.name" required placeholder="香港 VLESS 01" /></label>
          <label class="field"><span>协议</span><select v-model="form.protocol" required @change="syncServerConfigType"><option v-for="protocol in protocols" :key="protocol" :value="protocol">{{ protocol }}</option></select></label>
          <label class="field"><span>对外地址</span><input v-model.trim="form.address" required placeholder="edge.example.com" /></label>
          <label class="field"><span>监听端口</span><input v-model.number="form.port" type="number" min="1" max="65535" required /></label>
          <label class="field"><span>对外端口</span><input v-model.number="form.public_port" type="number" min="1" max="65535" required /></label>
          <label class="field"><span>计费倍率（千分值）</span><input v-model.number="form.multiplier_milli" type="number" min="1" max="100000" required /><small class="field-hint">1000 = 1×，1500 = 1.5×。套餐不再叠加倍率。</small></label>
          <label class="field"><span>排序</span><input v-model.number="form.sort_order" type="number" /></label>
          <label class="field"><span>父协议</span><select v-model.number="form.parent_protocol_id"><option :value="0">无</option><option v-for="candidate in parentCandidates" :key="candidate.id" :value="candidate.id">{{ candidate.name }}</option></select></label>
          <label class="check-field"><input v-model="form.is_active" type="checkbox" /><span>允许加入节点组</span></label>
        </div>
        <div class="config-grid">
          <label class="field"><span>服务端配置 JSON</span><textarea v-model="form.config" rows="12" spellcheck="false" required></textarea><small class="field-hint">必须是对象，且 type 与所选协议一致。</small></label>
          <label class="field"><span>客户端配置 JSON</span><textarea v-model="form.client_config" rows="12" spellcheck="false" required></textarea><small class="field-hint">交付给订阅客户端的协议参数。</small></label>
          <label class="field"><span>可选配置 JSON</span><textarea v-model="form.optional_config" rows="6" spellcheck="false"></textarea></label>
          <label class="field"><span>标签 JSON 数组</span><textarea v-model="form.tags" rows="6" spellcheck="false"></textarea></label>
        </div>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="editorOpen = false">取消</button><button class="button" form="protocol-form" type="submit" :disabled="saving">{{ saving ? '保存中…' : '保存配置' }}</button></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { createProtocolEndpoint, deployProtocolEndpoint, fetchNodes, fetchProtocolDeployments, fetchProtocolEndpoint, fetchProtocolEndpoints, updateProtocolEndpoint } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'

const protocols = ['vmess', 'vless', 'trojan', 'shadowsocks', 'hysteria2', 'mieru']
const nodes = ref<any[]>([]), endpoints = ref<any[]>([]), deployments = ref<any[]>([])
const loading = ref(false), saving = ref(false), deployingID = ref(0), editorOpen = ref(false)
const error = ref(''), message = ref('')
const emptyForm = () => ({ id: 0, node_id: 0, name: '', protocol: 'vless', address: '', port: 443, public_port: 443, multiplier_milli: 1000, sort_order: 0, parent_protocol_id: 0, is_active: false, config: '{\n  "type": "vless"\n}', client_config: '{}', optional_config: '{}', tags: '[]' })
const form = reactive<any>(emptyForm())
const activeCount = computed(() => endpoints.value.filter(endpoint => endpoint.is_active).length)
const usedNodeCount = computed(() => new Set(endpoints.value.map(endpoint => endpoint.node_id)).size)
const failedDeploymentCount = computed(() => deployments.value.filter(deployment => deployment.status === 'failed').length)
const parentCandidates = computed(() => endpoints.value.filter(endpoint => endpoint.node_id === form.node_id && endpoint.id !== form.id))

function nodeName(id: number) { return nodes.value.find(node => node.id === id)?.name || `VPS #${id}` }
function formatMultiplier(value: number) { return `${Number(value || 1000) / 1000}×` }
function latestDeployment(endpointID: number) { return deployments.value.find(deployment => deployment.protocol_endpoint_id === endpointID) }
function deploymentLabel(status?: string) { return status === 'succeeded' ? '已暂存' : status === 'failed' ? '失败' : status === 'running' ? '暂存中' : '未暂存' }
function deploymentTone(status?: string): 'success' | 'warning' | 'neutral' { return status === 'succeeded' ? 'success' : status === 'failed' || status === 'running' ? 'warning' : 'neutral' }

async function refresh() {
  loading.value = true; error.value = ''
  try {
    const [nodeItems, endpointItems, deploymentPage] = await Promise.all([fetchNodes(), fetchProtocolEndpoints(), fetchProtocolDeployments({ limit: 200 })])
    nodes.value = nodeItems; endpoints.value = endpointItems; deployments.value = deploymentPage.items || []
  } catch (e: any) { error.value = e?.response?.data?.message || '协议服务加载失败。' }
  finally { loading.value = false }
}
function openCreate() { Object.assign(form, emptyForm(), { node_id: nodes.value.find(node => node.is_enabled)?.id || nodes.value[0]?.id || 0, address: nodes.value.find(node => node.is_enabled)?.address || nodes.value[0]?.address || '' }); editorOpen.value = true }
async function openEdit(endpoint: any) { error.value = ''; try { const detail = await fetchProtocolEndpoint(endpoint.id); Object.assign(form, emptyForm(), detail, { parent_protocol_id: detail.parent_protocol_id || 0, config: detail.config || '{}', client_config: detail.client_config || '{}', optional_config: detail.optional_config || '{}', tags: detail.tags || '[]' }); editorOpen.value = true } catch (e: any) { error.value = e?.response?.data?.message || '协议详情加载失败。' } }
function syncServerConfigType() { try { const value = JSON.parse(form.config || '{}'); if (value && typeof value === 'object' && !Array.isArray(value)) { value.type = form.protocol; form.config = JSON.stringify(value, null, 2) } } catch { form.config = JSON.stringify({ type: form.protocol }, null, 2) } }
function validateJSON() { const server = JSON.parse(form.config); if (!server || Array.isArray(server) || typeof server !== 'object') throw new Error('服务端配置必须是 JSON 对象。'); if (String(server.type || '').toLowerCase() !== form.protocol) throw new Error('服务端配置的 type 必须与协议一致。'); for (const [label, value, array] of [['客户端配置', form.client_config, false], ['可选配置', form.optional_config || '{}', false], ['标签', form.tags || '[]', true]] as const) { const parsed = JSON.parse(value); if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) !== array) throw new Error(`${label} JSON 格式不正确。`) } }
async function save() { saving.value = true; error.value = ''; message.value = ''; try { validateJSON(); const payload = { node_id: form.node_id, name: form.name, protocol: form.protocol, address: form.address, port: form.port, public_port: form.public_port, multiplier_milli: form.multiplier_milli, sort_order: form.sort_order, parent_protocol_id: form.parent_protocol_id || null, is_active: form.is_active, config: form.config, client_config: form.client_config, optional_config: form.optional_config || '{}', tags: form.tags || '[]' }; if (form.id) await updateProtocolEndpoint(form.id, payload); else await createProtocolEndpoint(payload); editorOpen.value = false; message.value = '协议配置已保存，尚未自动部署。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || e?.message || '协议配置保存失败。' } finally { saving.value = false } }
async function deploy(endpoint: any) { deployingID.value = endpoint.id; error.value = ''; message.value = ''; try { const result = await deployProtocolEndpoint(endpoint.id); message.value = `${endpoint.name} 的单项配置已暂存到 ${nodeName(endpoint.node_id)}，耗时 ${result.latency_ms || 0}ms；尚未重载 Zero，请到节点页执行内核配置对齐。`; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '配置暂存失败，请检查节点 SSH 配置与连接验证。'; await refresh() } finally { deployingID.value = 0 } }
onMounted(refresh)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.protocol-metrics { margin-bottom: 16px; }.protocol-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }.protocol-card { display: grid; overflow: hidden; }.protocol-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 18px; }.protocol-title { display: flex; gap: 12px; min-width: 0; }.protocol-icon { width: 40px; height: 40px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 10px; color: var(--primary); background: var(--primary-soft); font-size: 19px; }.title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }.title-line h2 { margin: 0; font-size: 16px; }.protocol-title p { margin: 5px 0 0; color: var(--muted); font-family: var(--font-mono); font-size: 11px; overflow-wrap: anywhere; }.multiplier { color: var(--primary); font-size: 18px; }.protocol-meta { display: grid; grid-template-columns: repeat(3, 1fr); margin: 0; padding: 14px 18px; border-block: 1px solid var(--line); background: var(--surface-soft); }.protocol-meta div { display: grid; gap: 4px; }.protocol-meta dt { color: var(--muted); font-size: 10px; }.protocol-meta dd { margin: 0; font-size: 11px; font-weight: 650; }.deployment-error { margin: 12px 18px 0; padding: 9px; border-radius: 8px; color: var(--danger); background: var(--danger-soft); font-size: 11px; overflow-wrap: anywhere; }.protocol-actions { display: flex; justify-content: flex-end; gap: 8px; padding: 14px 18px; }.config-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }.config-grid textarea { font-family: var(--font-mono); font-size: 11px; }
@media (max-width: 900px) { .protocol-grid, .config-grid { grid-template-columns: 1fr; } }
</style>
