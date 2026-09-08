<template>
  <section class="standard-page">
    <PageHeader v-if="!embedded" title="前置转发服务" description="组合 A 入口与 B 落地协议，作为独立线路分配给节点组。用户鉴权与流量结算由 B 承载。">
      <template #actions><UiButton variant="secondary" :loading="loading" @click="load">刷新</UiButton><UiButton @click="edit()">创建入口</UiButton></template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" />
    <section class="panel">
      <DataTable v-if="entries.length" caption="前置转发服务" :row-count="entries.length" :min-width="800">
        <thead><tr><th class="table-primary-column">入口名称</th><th data-column-priority="2">A 入口节点</th><th data-column-priority="2">B 落地协议</th><th>对外入口</th><th data-column-priority="3">转发路径</th><th>分配范围</th><th>状态</th><th class="table-action-column">操作</th></tr></thead>
        <tbody><tr v-for="entry in entries" :key="entry.id">
          <td class="table-primary-column"><strong>{{ entry.name }}</strong></td>
          <td data-column-priority="2"><TableText :value="entry.node_name" /></td><td data-column-priority="2"><TableText :value="entry.endpoint_name" /></td>
          <td><EndpointAddress :address="entry.address" :port="entry.public_port" /></td>
          <td class="nowrap" data-column-priority="3">{{ entry.proxy_pool_id ? '共享代理池' : entry.has_path ? '独立代理路径' : '直连落地' }}</td>
          <td><TableText :value="entry.node_group_names?.join('、') || '未分配，不下发'" /></td>
          <td><StatusBadge :tone="entry.last_error ? 'danger' : entry.pending ? 'warning' : entry.enabled ? 'success' : 'neutral'">{{ entry.last_error ? '发布失败，等待重试' : entry.pending ? '等待发布' : entry.enabled ? '已发布' : '已停用' }}</StatusBadge><small v-if="entry.last_error" class="entry-error">{{ entry.last_error }}</small></td>
          <td class="table-action-column"><RowActions :label="`${entry.name} 的操作`"><UiButton size="sm" variant="ghost" @click="edit(entry)">编辑</UiButton><UiButton size="sm" variant="danger" @click="remove(entry)">删除</UiButton></RowActions></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else title="还没有前置入口" description="选择 A 的端口承载 B 的父协议；握手和认证始终由 B 完成。" />
    </section>
    <ModalDialog :open="open" :title="editing ? '编辑前置转发服务' : '创建前置转发服务'" :busy="saving" size="xl" @close="open = false">
      <div class="modal-form">
        <p class="entry-preview">A 只监听转发端口，不创建 SS、VLESS 等协议监听；客户端认证与协议握手由父协议所在的 B 完成。关联父协议不会授予 B 的使用权限。</p>
        <p v-if="formError" class="entry-error" role="alert">{{ formError }}</p>
        <FormField label="入口名称" required full><UiInput v-model.trim="form.name" maxlength="80" placeholder="例如：香港入口" /></FormField>
        <FormField label="入口节点 A" required><NodeLookup v-model="form.node_id" @select="node => { if(node && !editing) form.address = node.address || '' }" /></FormField>
        <FormField label="落地节点 B" required><NodeLookup v-model="landingNode" /></FormField>
        <FormField label="父协议（B 的实际协议）" required full :hint="endpointError || '选择转发目标，不继承落地协议的节点组权限。'"><UiSelect v-model="form.endpoint_id" :options="endpointOptions" :disabled="!landingNode || endpointLoading" placeholder="选择落地协议" /></FormField>
        <FormField label="A 的对外地址" required full hint="客户端连接的 IP 或域名。A 的 Zero 将接收端口转发配置。"><UiInput v-model.trim="form.address" placeholder="entry.example.com" /></FormField>
        <FormField label="A 的监听端口" required><UiInput v-model.number="form.port" type="number" min="1" max="65535" /></FormField>
        <FormField label="A 的对外端口" required hint="存在端口映射时填写映射后的端口。"><UiInput v-model.number="form.public_port" type="number" min="1" max="65535" /></FormField>
        <FormField label="转发传输" hint="旧内核和仅 TCP 的代理路径可选 TCP；Hysteria2 必须选择 TCP/UDP。"><UiSelect v-model="form.network" :options="[{label: 'TCP 与 UDP', value: 'tcp_udp'}, {label: '仅 TCP', value: 'tcp'}]" /></FormField>
        <FormField label="服务状态"><UiSelect v-model="form.enabled" :options="[{ label: '启用', value: true }, { label: '停用', value: false }]" /></FormField>
        <FormField label="A 到 B 的路径" full hint="直连落地不需要代理凭据。代理路径支持 Zero 的出口、自动测速组和链式代理组。"><UiSelect v-model="pathMode" :options="pathOptions" /></FormField>
        <FormField v-if="pathMode === 'pool'" label="共享代理池" :hint="poolError || '只使用入口节点 A 自己的代理池。'" required full><UiSelect v-model="poolID" :options="poolOptions" :disabled="poolLoading" placeholder="选择 A 的共享代理池" /><a v-if="form.node_id" :href="`/admin/nodes?node=${form.node_id}&tab=pools`">管理 A 的共享代理池</a></FormField>
        <FormField label="关联节点组" full><NodeGroupMembershipEditor v-model="memberships" :can-add="form.enabled" /></FormField>
        <p class="entry-preview">客户端使用 A 的地址和端口，协议参数与凭据沿用 B。入口授权不自动生成 B 的凭据。套餐还需显式授权 B，才会生成可用的客户端节点；流量由 B 计量。</p>
      </div>
      <template #footer><UiButton variant="secondary" @click="open = false">取消</UiButton><UiButton :loading="saving" @click="save">保存并发布</UiButton></template>
    </ModalDialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { fetchNetworkEntries, saveNetworkEntry, deleteNetworkEntry, fetchProtocolEndpoints, fetchNodeProxyPools, type NetworkEntry, type ProtocolEndpointNodeGroupMembership } from '../api/client'
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
import NodeGroupMembershipEditor from '../components/NodeGroupMembershipEditor.vue'
import {buildProtocolNodeGroupMembershipChanges} from '../utils/protocolNodeGroupMembership'
withDefaults(defineProps<{embedded?:boolean}>(),{embedded:false})
const entries = ref<NetworkEntry[]>([]), loading = ref(false), saving = ref(false), open = ref(false)
const error = ref(''), message = ref(''), formError = ref(''), endpointError = ref(''), endpointLoading = ref(false)
const editing = ref<NetworkEntry | null>(null), landingNode = ref(0), endpointOptions = ref<{label: string; value: number}[]>([])
const form = reactive({network: 'tcp_udp', name: '', node_id: 0, endpoint_id: 0, address: '', port: 10000, public_port: 10000, enabled: true})
const pathMode = ref('direct')
const poolID=ref(0),poolLoading=ref(false),poolError=ref(''),poolOptions=ref<{label:string;value:number}[]>([])
const memberships=ref<ProtocolEndpointNodeGroupMembership[]>([]),originalMemberships=ref<ProtocolEndpointNodeGroupMembership[]>([])
const pathOptions = computed(() => [...(editing.value?.has_path ? [{label: '保留现有代理路径', value: 'keep'}] : []), {label: '直连落地', value: 'direct'}, {label: '使用 A 的共享代理池', value: 'pool'}])

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
  editing.value = entry || null; landingNode.value = entry?.landing_node_id || 0; formError.value = ''; pathMode.value = entry?.proxy_pool_id ? 'pool' : entry?.has_path ? 'keep' : 'direct';poolID.value=entry?.proxy_pool_id||0;memberships.value=(entry?.node_group_memberships||[]).map(item=>({...item}));originalMemberships.value=memberships.value.map(item=>({...item}))
  Object.assign(form, entry ? {network: entry.network || 'tcp_udp', name: entry.name, node_id: entry.node_id, endpoint_id: entry.endpoint_id, address: entry.address, port: entry.port, public_port: entry.public_port, enabled: entry.enabled} : {network: 'tcp_udp', name: '', node_id: 0, endpoint_id: 0, address: '', port: 10000, public_port: 10000, enabled: true})
  open.value = true
}
async function save() {
  saving.value = true; formError.value = ''
  try {
    const payload: Record<string, unknown> = {...form, revision: editing.value?.revision || 0}
    payload.parent_protocol_id=form.endpoint_id;payload.node_group_membership_changes=buildProtocolNodeGroupMembershipChanges(originalMemberships.value,memberships.value)
    if(pathMode.value === 'direct') { payload.path_config = {};payload.proxy_pool_id=0 }
    if(pathMode.value === 'pool') { if(!poolID.value)throw new Error('请选择 A 的共享代理池。');payload.proxy_pool_id=poolID.value;payload.path_config={} }
    await saveNetworkEntry(editing.value?.id || 0, payload); open.value = false; message.value = '已保存服务和节点组关联，正在排队发布。'; await load()
  } catch(cause) { formError.value = failure(cause) } finally { saving.value = false }
}
async function remove(entry: NetworkEntry) {
  if(!await confirmAction({title: '删除前置入口', message: `删除「${entry.name}」及节点组关联，并排队撤除 A 的转发配置，B 的直连线路继续保留。`, confirmText: '删除入口', tone: 'danger'})) return
  try { await deleteNetworkEntry(entry.id); message.value = '已删除入口，正在排队撤除节点配置。'; await load() } catch(cause) { error.value = failure(cause) }
}
let poolSequence=0
watch(()=>form.node_id,async (id,previous)=>{const seq=++poolSequence;poolOptions.value=[];poolError.value='';if(previous && id !== editing.value?.node_id)poolID.value=0;if(!id)return;poolLoading.value=true;try{const pools=await fetchNodeProxyPools(id);if(seq===poolSequence)poolOptions.value=pools.map(pool=>({label:pool.name,value:pool.id}))}catch(cause){if(seq===poolSequence)poolError.value=failure(cause)}finally{if(seq===poolSequence)poolLoading.value=false}})
defineExpose({edit,load})
onMounted(load)
</script>
<style scoped>
.modal-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.modal-form>p{grid-column:1/-1}.modal-form :deep(.form-field-full){grid-column:1/-1}@media(max-width:640px){.modal-form{grid-template-columns:1fr}}.standard-page,.panel{min-width:0;max-width:100%}.panel{overflow:hidden}.path-input{width:100%;font-family:monospace;padding:12px;border:1px solid var(--line);border-radius:8px;resize:vertical}.entry-error{color:var(--danger);overflow-wrap:anywhere}.entry-preview{grid-column:1/-1;color:var(--text-secondary)}pre{white-space:pre-wrap;overflow-wrap:anywhere}.nowrap{white-space:nowrap}
</style>
