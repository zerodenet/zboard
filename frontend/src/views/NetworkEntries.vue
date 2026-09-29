<template>
  <section class="standard-page">
    <PageHeader v-if="!embedded" title="前置转发服务" description="组合 A 入口与 B 落地协议，作为独立线路分配给节点组。用户鉴权与流量结算由 B 承载。">
      <template #actions><UiButton variant="secondary" :loading="loading" @click="load">刷新</UiButton><UiButton @click="edit()">创建入口</UiButton></template>
    </PageHeader>
    <TransientFeedback :success="message" :error="error" />
    <section v-if="!editorOnly" class="panel">
      <DataTable v-if="entries.length" caption="前置转发服务" :row-count="entries.length" :min-width="800">
        <thead><tr><th class="table-primary-column">入口名称</th><th data-column-priority="2">A 入口节点</th><th data-column-priority="2">B 落地协议</th><th>对外入口</th><th data-column-priority="3">转发路径</th><th>分配范围</th><th>状态</th><th class="table-action-column">操作</th></tr></thead>
        <tbody><tr v-for="entry in entries" :key="entry.id">
          <td class="table-primary-column"><strong>{{ entry.name }}</strong></td>
          <td data-column-priority="2"><TableText :value="entry.deployment_mode === 'external' ? '外部转发' : entry.node_name" /></td><td data-column-priority="2"><TableText :value="entry.endpoint_name" /></td>
          <td><EndpointAddress :address="entry.address" :port="entry.public_port" /></td>
          <td class="nowrap" data-column-priority="3">{{ entry.deployment_mode === 'external' ? '外部维护' : entry.proxy_pool_id ? '共享代理池' : entry.has_path ? '独立代理路径' : '直连落地' }}</td>
          <td><TableText :value="entry.node_group_names?.join('、') || '未分配，不下发'" /></td>
          <td><StatusBadge :tone="entry.last_error ? 'danger' : entry.pending ? 'warning' : entry.enabled ? 'success' : 'neutral'">{{ entry.last_error ? '发布失败，等待重试' : entry.pending ? '等待发布' : entry.enabled ? '已启用' : '已停用' }}</StatusBadge><small v-if="entry.last_error" class="entry-error">{{ entry.last_error }}</small></td>
          <td class="table-action-column"><RowActions :label="`${entry.name} 的操作`"><UiButton size="sm" variant="ghost" @click="edit(entry)">编辑</UiButton><UiButton size="sm" variant="danger" @click="remove(entry)">删除</UiButton></RowActions></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else title="还没有前置入口" description="登记外部入口，或由面板托管 A 的端口转发；握手和认证由父协议完成。" />
    </section>
    <ModalDialog :open="open" :title="editing ? '编辑前置转发服务' : '创建前置转发服务'" :busy="saving" size="xl" @close="open = false">
      <div class="modal-form">
        <p class="entry-preview">入口只提供转发地址，客户端认证与协议握手由父协议完成。入口授权会生成父协议凭据，但不会额外下发落地直连地址。</p>
        <p v-if="formError" class="entry-error" role="alert">{{ formError }}</p>
        <FormField label="入口名称" required full hint="客户端订阅直接显示此名称，不再追加落地协议名称。展示位置可在「订阅展示顺序」中调整。"><UiInput v-model.trim="form.name" maxlength="80" placeholder="例如：香港入口" /></FormField>
        <FormField label="转发部署方式" full><UiSelect v-model="form.deployment_mode" :options="[{label:'外部已部署转发',value:'external'},{label:'面板托管 Zero 转发',value:'managed'}]" /></FormField>
        <FormField v-if="form.deployment_mode === 'managed'" label="入口节点 A" required><NodeLookup v-model="form.node_id" @select="node => { if(node && !editing) form.address = node.address || '' }" /></FormField>
        <FormField label="落地节点 B" required><NodeLookup v-model="landingNode" /></FormField>
        <FormField label="父协议（B 的实际协议）" required full :hint="endpointError || '入口继承父协议的握手参数与用户凭据，交付范围独立管理。'"><UiSelect v-model="form.endpoint_id" :options="endpointOptions" :disabled="!landingNode || endpointLoading" placeholder="选择落地协议" /></FormField>
        <FormField label="客户端入口地址" required full :hint="form.deployment_mode === 'external' ? '填写外部转发的客户端地址，面板不部署入口服务器。' : '客户端连接的 IP 或域名，面板会发布 A 的转发配置。'"><UiInput v-model.trim="form.address" placeholder="entry.example.com" /></FormField>
        <FormField v-if="form.deployment_mode === 'managed'" label="A 的监听端口" required><PortInput v-model="form.port" /></FormField>
        <FormField label="客户端入口端口" required hint="存在端口映射时填写映射后的端口。"><PortInput v-model="form.public_port" /></FormField>
        <FormField label="转发传输" hint="旧内核和仅 TCP 的代理路径可选 TCP；Hysteria2 必须选择 TCP/UDP。"><UiSelect v-model="form.network" :options="[{label: 'TCP 与 UDP', value: 'tcp_udp'}, {label: '仅 TCP', value: 'tcp'}]" /></FormField>
        <FormField label="服务状态"><UiSelect v-model="form.enabled" :options="[{ label: '启用', value: true }, { label: '停用', value: false }]" /></FormField>
        <FormField v-if="form.deployment_mode === 'managed'" label="A 到 B 的路径" full hint="直连落地不需要代理凭据。代理路径支持 Zero 的出口、自动测速组和链式代理组。"><UiSelect v-model="pathMode" :options="pathOptions" /></FormField>
        <FormField v-if="form.deployment_mode === 'managed' && pathMode === 'pool'" label="共享代理池" :hint="poolError || '只使用入口节点 A 自己的代理池。'" required full><UiSelect v-model="poolID" :options="poolOptions" :disabled="poolLoading" placeholder="选择 A 的共享代理池" /><a v-if="form.node_id" :href="`/admin/nodes?node=${form.node_id}&tab=pools`">管理 A 的共享代理池</a></FormField>
        <FormField label="关联节点组" full><NodeGroupMembershipEditor v-model="memberships" :can-add="form.enabled" /></FormField>
        <p class="entry-preview">客户端使用入口地址和端口，协议参数与凭据沿用父协议。将入口加入套餐的节点组后，系统会生成父协议凭据，只下发此入口。落地直连只有单独加入节点组才会下发；流量按父协议计量。</p>
      </div>
      <template #footer><UiButton variant="secondary" @click="open = false">取消</UiButton><UiButton :loading="saving" @click="save">{{ form.deployment_mode === 'external' ? '保存入口' : '保存并发布' }}</UiButton></template>
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
import PortInput from '../components/PortInput.vue'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import NodeGroupMembershipEditor from '../components/NodeGroupMembershipEditor.vue'
import {buildProtocolNodeGroupMembershipChanges} from '../utils/protocolNodeGroupMembership'
const props=withDefaults(defineProps<{embedded?:boolean;editorOnly?:boolean}>(),{embedded:false,editorOnly:false})
const emit=defineEmits<{saved:[]}>()
const entries = ref<NetworkEntry[]>([]), loading = ref(false), saving = ref(false), open = ref(false)
const error = ref(''), message = ref(''), formError = ref(''), endpointError = ref(''), endpointLoading = ref(false)
const editing = ref<NetworkEntry | null>(null), landingNode = ref(0), endpointOptions = ref<{label: string; value: number}[]>([])
const form = reactive({deployment_mode: 'external' as 'external' | 'managed', network: 'tcp_udp', name: '', node_id: 0, endpoint_id: 0, address: '', port: 10000, public_port: 10000, enabled: true})
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
  Object.assign(form, entry ? {deployment_mode: entry.deployment_mode || 'managed', network: entry.network || 'tcp_udp', name: entry.name, node_id: entry.node_id, endpoint_id: entry.endpoint_id, address: entry.address, port: entry.port, public_port: entry.public_port, enabled: entry.enabled} : {deployment_mode: 'external', network: 'tcp_udp', name: '', node_id: 0, endpoint_id: 0, address: '', port: 10000, public_port: 10000, enabled: true})
  open.value = true
}
async function save() {
  saving.value = true; formError.value = ''
  try {
    const payload: Record<string, unknown> = {...form, revision: editing.value?.revision || 0}
    payload.parent_protocol_id=form.endpoint_id;payload.node_group_membership_changes=buildProtocolNodeGroupMembershipChanges(originalMemberships.value,memberships.value)
    if(form.deployment_mode === 'external') {payload.node_id=0;payload.port=form.public_port;payload.proxy_pool_id=0;payload.path_config={}}
    else if(pathMode.value === 'direct') { payload.path_config = {};payload.proxy_pool_id=0 }
    if(form.deployment_mode === 'managed' && pathMode.value === 'pool') { if(!poolID.value)throw new Error('请选择 A 的共享代理池。');payload.proxy_pool_id=poolID.value;payload.path_config={} }
    await saveNetworkEntry(editing.value?.id || 0, payload); open.value = false; message.value = '已保存服务和节点组关联。'; emit('saved'); if(!props.editorOnly) await load()
  } catch(cause) { formError.value = failure(cause) } finally { saving.value = false }
}
async function remove(entry: NetworkEntry) {
  if(!await confirmAction({title: '删除前置入口', message: `删除「${entry.name}」及节点组关联。${entry.deployment_mode === 'external' ? '外部转发由您自行停止。' : '面板将撤除入口服务器的转发配置。'}父协议及单独授权的直连线路保留；仅依赖此入口的授权会撤回。`, confirmText: '删除入口', tone: 'danger'})) return
  try { await deleteNetworkEntry(entry.id); message.value = entry.deployment_mode === 'external' ? '已删除入口并撤回关联授权。' : '已删除入口，正在撤回关联授权和转发配置。'; emit('saved'); if(!props.editorOnly) await load() } catch(cause) { error.value = failure(cause) }
}
let poolSequence=0
watch(()=>form.node_id,async (id,previous)=>{const seq=++poolSequence;poolOptions.value=[];poolError.value='';if(previous && id !== editing.value?.node_id)poolID.value=0;if(!id)return;poolLoading.value=true;try{const pools=await fetchNodeProxyPools(id);if(seq===poolSequence)poolOptions.value=pools.map(pool=>({label:pool.name,value:pool.id}))}catch(cause){if(seq===poolSequence)poolError.value=failure(cause)}finally{if(seq===poolSequence)poolLoading.value=false}})
defineExpose({edit,load,remove})
// The combined page loads a bounded forward projection itself.
onMounted(()=>{if(!props.editorOnly) void load()})
</script>
<style scoped>
.modal-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.modal-form>p{grid-column:1/-1}.modal-form :deep(.form-field-full){grid-column:1/-1}@media(max-width:640px){.modal-form{grid-template-columns:1fr}}.standard-page,.panel{min-width:0;max-width:100%}.panel{overflow:hidden}.path-input{width:100%;font-family:monospace;padding:12px;border:1px solid var(--line);border-radius:8px;resize:vertical}.entry-error{color:var(--danger);overflow-wrap:anywhere}.entry-preview{grid-column:1/-1;color:var(--text-secondary)}pre{white-space:pre-wrap;overflow-wrap:anywhere}.nowrap{white-space:nowrap}
</style>
