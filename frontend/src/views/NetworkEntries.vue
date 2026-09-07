<template>
  <section class="standard-page">
    <PageHeader title="网络前置" description="让 A 转发到 B 的落地协议。订阅同时保留直连与前置线路，使用权限、鉴权及扣流量均归属 B。">
      <template #actions><UiButton variant="secondary" :loading="loading" @click="load">刷新</UiButton><UiButton @click="edit()">创建入口</UiButton></template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" />
    <section class="panel">
      <DataTable v-if="entries.length" caption="网络前置入口" :row-count="entries.length" :min-width="800">
        <thead><tr><th class="table-primary-column">入口名称</th><th data-column-priority="2">A 入口节点</th><th data-column-priority="2">B 落地协议</th><th>对外入口</th><th data-column-priority="3">转发路径</th><th>状态</th><th class="table-action-column">操作</th></tr></thead>
        <tbody><tr v-for="entry in entries" :key="entry.id">
          <td class="table-primary-column"><strong>{{ entry.name }}</strong></td>
          <td data-column-priority="2"><TableText :value="entry.node_name" /></td><td data-column-priority="2"><TableText :value="entry.endpoint_name" /></td>
          <td><EndpointAddress :address="entry.address" :port="entry.public_port" /></td>
          <td class="nowrap" data-column-priority="3">{{ entry.has_path ? '代理路径' : '直连落地' }}</td>
          <td><StatusBadge :tone="entry.last_error ? 'danger' : entry.pending ? 'warning' : entry.enabled ? 'success' : 'neutral'">{{ entry.last_error ? '发布失败，等待重试' : entry.pending ? '等待发布' : entry.enabled ? '已发布' : '已停用' }}</StatusBadge><small v-if="entry.last_error" class="entry-error">{{ entry.last_error }}</small></td>
          <td class="table-action-column"><RowActions :label="`${entry.name} 的操作`"><UiButton size="sm" variant="ghost" @click="edit(entry)">编辑</UiButton><UiButton size="sm" variant="danger" @click="remove(entry)">删除</UiButton></RowActions></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else title="还没有前置入口" description="先在 B 创建落地协议并分配节点组，再为它创建 A 的转发入口。" />
    </section>
    <ModalDialog :open="open" :title="editing ? '编辑前置入口' : '创建前置入口'" :busy="saving" @close="open = false">
      <div class="modal-form">
        <p v-if="formError" class="entry-error" role="alert">{{ formError }}</p>
        <FormField label="入口名称" required><UiInput v-model.trim="form.name" maxlength="80" placeholder="例如：香港入口" /></FormField>
        <FormField label="入口节点 A" required><NodeLookup v-model="form.node_id" /></FormField>
        <FormField label="落地节点 B" required><NodeLookup v-model="landingNode" /></FormField>
        <FormField label="B 的落地协议" required :hint="endpointError || '沿用该协议所属节点组的使用权限。'"><UiSelect v-model="form.endpoint_id" :options="endpointOptions" :disabled="!landingNode || endpointLoading" placeholder="选择落地协议" /></FormField>
        <FormField label="A 的对外地址" required hint="客户端连接的 IP 或域名。A 需运行支持 TCP/UDP 网络前置的新版 Zero。"><UiInput v-model.trim="form.address" placeholder="entry.example.com" /></FormField>
        <FormField label="A 的监听端口" required><UiInput v-model.number="form.port" type="number" min="1" max="65535" /></FormField>
        <FormField label="A 的对外端口" required hint="存在端口映射时填写映射后的端口。"><UiInput v-model.number="form.public_port" type="number" min="1" max="65535" /></FormField>
        <FormField label="转发传输" hint="旧内核和仅 TCP 的代理路径可选 TCP；Hysteria2 必须选择 TCP/UDP。"><UiSelect v-model="form.network" :options="[{label: 'TCP 与 UDP', value: 'tcp_udp'}, {label: '仅 TCP', value: 'tcp'}]" /></FormField>
        <FormField label="服务状态"><UiSelect v-model="form.enabled" :options="[{ label: '启用', value: true }, { label: '停用', value: false }]" /></FormField>
        <FormField label="A 到 B 的路径" full hint="直连落地不需要代理凭据。代理路径支持 Zero 的出口、自动测速组和链式代理组。"><UiSelect v-model="pathMode" :options="pathOptions" /></FormField>
        <FormField v-if="pathMode === 'replace'" label="代理路径配置" full hint="填写 outbounds、outbound_groups 和 target。凭据加密保存，不会下发给客户端或回显。">
          <UiTextarea v-model="pathText" class="path-input" rows="12" spellcheck="false" aria-label="代理路径配置" :placeholder="pathExample" />
          <details><summary>查看配置格式</summary><pre>{{ pathExample }}</pre><p>target 指向出口或组。url_test 选择可用出口；relay 组按顺序组成代理链。这里只配置 A 到 B 的路径，不包含 B 的用户凭据。</p></details>
        </FormField>
        <p class="entry-preview">订阅展示：B 的原线路 +「{{ form.name || '入口名称' }} · B 的协议名称」。没有 B 使用权限的用户不会获得这两条线路。</p>
      </div>
      <template #footer><UiButton variant="secondary" @click="open = false">取消</UiButton><UiButton :loading="saving" @click="save">保存并发布</UiButton></template>
    </ModalDialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { fetchNetworkEntries, saveNetworkEntry, deleteNetworkEntry, fetchProtocolEndpoints, type NetworkEntry } from '../api/client'
import { confirmAction } from '../utils/feedback'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import EndpointAddress from '../components/EndpointAddress.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import NodeLookup from '../components/NodeLookup.vue'
import PageHeader from '../components/PageHeader.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TableText from '../components/TableText.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import UiTextarea from '../components/UiTextarea.vue'
const entries = ref<NetworkEntry[]>([]), loading = ref(false), saving = ref(false), open = ref(false)
const error = ref(''), message = ref(''), formError = ref(''), endpointError = ref(''), endpointLoading = ref(false)
const editing = ref<NetworkEntry | null>(null), landingNode = ref(0), endpointOptions = ref<{label: string; value: number}[]>([])
const form = reactive({network: 'tcp_udp', name: '', node_id: 0, endpoint_id: 0, address: '', port: 10000, public_port: 10000, enabled: true})
const pathMode = ref('direct'), pathText = ref('')
const pathOptions = computed(() => [...(editing.value?.has_path ? [{label: '保留现有代理路径', value: 'keep'}] : []), {label: '直连落地', value: 'direct'}, {label: '设置代理路径', value: 'replace'}])
const pathExample = JSON.stringify({outbounds: [{tag: 'proxy', protocol: {type: 'socks5', server: 'proxy.example.com', port: 1080}}], outbound_groups: [], target: 'proxy'}, null, 2)
function failure(cause: any) { const data = cause?.response?.data; const fields = data?.error?.fields; return [data?.message || cause?.message || '操作失败。', ...Object.values(fields || {})].join(' ') }
async function load() { loading.value = true; error.value = ''; try { entries.value = await fetchNetworkEntries() } catch(cause) { error.value = failure(cause) } finally { loading.value = false } }
let selectionSequence = 0
watch(landingNode, async id => {
  const sequence = ++selectionSequence; endpointOptions.value = []; endpointError.value = ''; form.endpoint_id = 0
  if (!id) return
  endpointLoading.value = true
  try { const rows = await fetchProtocolEndpoints(id); if(sequence !== selectionSequence) return; endpointOptions.value = rows.filter((row: any) => row.is_active).map((row: any) => ({label: row.name, value: row.id})); if(editing.value && endpointOptions.value.some(row => row.value === editing.value?.endpoint_id)) form.endpoint_id = editing.value.endpoint_id }
  catch(cause) { if(sequence === selectionSequence) endpointError.value = failure(cause) }
  finally { if(sequence === selectionSequence) endpointLoading.value = false }
})
function edit(entry?: NetworkEntry) {
  editing.value = entry || null; landingNode.value = entry?.landing_node_id || 0; formError.value = ''; pathText.value = ''; pathMode.value = entry?.has_path ? 'keep' : 'direct'
  Object.assign(form, entry ? {network: entry.network || 'tcp_udp', name: entry.name, node_id: entry.node_id, endpoint_id: entry.endpoint_id, address: entry.address, port: entry.port, public_port: entry.public_port, enabled: entry.enabled} : {network: 'tcp_udp', name: '', node_id: 0, endpoint_id: 0, address: '', port: 10000, public_port: 10000, enabled: true})
  open.value = true
}
async function save() {
  saving.value = true; formError.value = ''
  try {
    const payload: Record<string, unknown> = {...form, revision: editing.value?.revision || 0}
    if(pathMode.value === 'direct') payload.path_config = {}
    if(pathMode.value === 'replace') { if(!pathText.value.trim()) throw new Error('请填写代理路径配置。'); payload.path_config = JSON.parse(pathText.value) }
    await saveNetworkEntry(editing.value?.id || 0, payload); open.value = false; message.value = '已保存并排队发布，可刷新查看结果。'; await load()
  } catch(cause) { formError.value = failure(cause) } finally { saving.value = false }
}
async function remove(entry: NetworkEntry) {
  if(!await confirmAction({title: '删除前置入口', message: `删除「${entry.name}」并撤除 A 的转发配置，B 的直连线路继续保留。`, confirmText: '删除入口', tone: 'danger'})) return
  try { await deleteNetworkEntry(entry.id); message.value = '已删除入口，正在排队撤除节点配置。'; await load() } catch(cause) { error.value = failure(cause) }
}
onMounted(load)
</script>
<style scoped>
.standard-page,.panel{min-width:0;max-width:100%}.panel{overflow:hidden}.path-input{width:100%;font-family:monospace;padding:12px;border:1px solid var(--line);border-radius:8px;resize:vertical}.entry-error{color:var(--danger);overflow-wrap:anywhere}.entry-preview{grid-column:1/-1;color:var(--text-secondary)}pre{white-space:pre-wrap;overflow-wrap:anywhere}.nowrap{white-space:nowrap}
</style>
