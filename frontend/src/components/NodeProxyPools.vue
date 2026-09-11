<template>
 <section class="stack" aria-label="节点共享代理池">
  <header class="pool-heading"><div><h3>共享代理池</h3><p>供本节点的前置端口转发引用。每条入口可独立选择直连落地，或使用这里的代理池；客户端握手和认证始终在落地完成。</p></div><UiButton @click="edit()">创建代理池</UiButton></header>
  <PageAlert v-if="error" tone="danger" title="代理池操作失败">{{error}}</PageAlert>
  <p v-if="loading">正在加载代理池…</p>
  <p v-else-if="!pools.length">尚未配置代理池。前置入口默认直连落地，不需要创建池。</p>
  <article v-for="pool in pools" :key="pool.id" class="pool-heading"><div><strong>{{pool.name}}</strong><p>{{pool.entry_count}} 条前置服务引用<span v-if="pool.subscription_configured"> · 订阅维护 · {{pool.subscription_node_count || 0}} 个节点</span></p><p v-if="pool.last_sync_error" class="pool-sync-error">最近同步失败：{{pool.last_sync_error}}</p></div><div><UiButton v-if="pool.subscription_configured" variant="secondary" :loading="syncingID === pool.id" @click="sync(pool)">同步</UiButton><UiButton variant="secondary" @click="edit(pool)">编辑</UiButton><UiButton variant="danger" @click="remove(pool)">删除</UiButton></div></article>
  <ModalDialog :open="open" :title="editing ? '编辑共享代理池' : '创建共享代理池'" :busy="saving" size="xl" @close="close">
   <div class="stack"><PageAlert v-if="formError" tone="danger" title="无法保存代理池">{{formError}}</PageAlert>
    <FormField label="代理池名称" required><UiInput v-model.trim="name" maxlength="80" /></FormField>
    <p v-if="configLoading">正在读取现有代理和凭据…</p>
    <div v-if="open && !configLoading && configReady" class="pool-mode-actions" role="tablist" aria-label="代理池维护方式"><UiButton :variant="maintenanceMode === 'subscription' ? 'primary' : 'secondary'" role="tab" :aria-selected="maintenanceMode === 'subscription'" @click="maintenanceMode = 'subscription'">订阅维护</UiButton><UiButton :variant="maintenanceMode === 'manual' ? 'primary' : 'secondary'" role="tab" :aria-selected="maintenanceMode === 'manual'" @click="maintenanceMode = 'manual'">原始配置导入</UiButton></div>
    <section v-if="open && !configLoading && configReady && maintenanceMode === 'subscription'" class="stack pool-subscription">
     <PageAlert tone="info" title="订阅更新会完整替换池成员">同步成功后才替换当前配置并发布；下载、解析或 Zero 校验失败时继续使用上一版。需要临时覆盖时，可切换到“原始配置导入”，订阅设置会保留。</PageAlert>
     <FormField label="订阅地址" required full><UiInput v-model.trim="subscription.url" aria-label="订阅地址" type="url" maxlength="2048" autocomplete="off" placeholder="https://example.com/subscription/token" /></FormField>
     <div class="form-grid"><FormField label="订阅格式"><UiSelect v-model="subscription.format" :options="subscriptionFormats" /></FormField><FormField label="自动同步间隔" hint="5 分钟至 7 天"><UiNumberInput v-model="subscription.sync_interval_seconds" :min="300" :max="604800" suffix=" 秒" /></FormField></div>
     <SubscriptionUserAgent v-model="subscription.user_agent" />
     <aside class="pool-subscription-support" aria-label="订阅格式帮助"><div><strong>支持配置订阅与节点链接</strong><span>格式要求与使用限制请查看文档。</span></div><a href="https://docs.zerodenet.org/projects/zboard/guides/network-fronting" target="_blank" rel="noopener noreferrer">查看文档 ↗</a></aside>
     <label class="check-field"><UiCheckbox v-model="subscription.auto_sync" /><span><strong>自动同步并发布</strong><br /><small class="field-hint">到达间隔后后台拉取；失败不会清空当前代理池。</small></span></label>
     <div v-if="editing && currentSubscription?.configured" class="pool-sync-status"><p>当前 {{currentSubscription.node_count}} 个订阅节点<span v-if="currentSubscription.last_sync_at"> · 上次成功：{{new Date(currentSubscription.last_sync_at).toLocaleString()}}</span></p><div><UiButton variant="secondary" :loading="syncingID === editing.id" @click="syncEditing">保存并立即同步</UiButton><UiButton variant="ghost" @click="detachSubscription">移除订阅维护</UiButton></div></div>
     <PageAlert v-if="currentSubscription?.last_sync_error" tone="danger" title="最近同步失败">{{currentSubscription.last_sync_error}}</PageAlert>
    </section>
    <ProxyPoolEditor v-if="open && !configLoading && configReady && maintenanceMode === 'manual'" :key="editorKey" ref="editor" :config="initialConfig" />
    <section v-if="editing && configReady" class="stack pool-runtime">
     <header class="pool-heading"><div><h3>节点已下发 RAW</h3><p>从节点当前配置文件读取本池及关联转发入口。此快照与上面的编辑草稿独立，保存后需发布成功才会更新。</p></div><UiButton variant="secondary" :loading="runtimeLoading" @click="readRuntime">读取节点配置</UiButton></header>
     <PageAlert v-if="runtimeError" tone="danger" title="节点配置读取失败">{{ runtimeError }}</PageAlert>
     <template v-if="runtime">
      <p>来源：{{ runtime.source }} · 读取时间：{{ new Date(runtime.read_at).toLocaleString() }}</p>
      <p v-if="!runtime.present">当前文件中没有此代理池，可能尚未发布，或没有启用的入口引用。</p>
      <FormField label="已下发配置快照" full><UiTextarea :model-value="JSON.stringify(runtime.config, null, 2)" aria-label="已下发配置快照" readonly rows="16" spellcheck="false" /></FormField>
      <p class="pool-hash">节点配置 SHA-256：{{ runtime.sha256 }}</p>
     </template>
    </section>
   </div>
   <template #footer><UiButton variant="secondary" @click="close">取消</UiButton><UiButton :loading="saving" :disabled="configLoading || !configReady" @click="save">{{maintenanceMode === 'manual' ? '保存并发布到本节点' : '保存订阅设置'}}</UiButton></template>
  </ModalDialog>
 </section>
</template>
<script setup lang="ts">
import {ref,watch} from 'vue'
import {fetchNodeProxyPools,fetchNodeProxyPoolConfig,fetchNodeProxyPoolRuntime,saveNodeProxyPool,syncNodeProxyPool,deleteNodeProxyPool,type NodeProxyPool,type NodeProxyPoolRuntime,type NodeProxyPoolSubscription} from '../api/client'
import {confirmAction} from '../utils/feedback'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiNumberInput from './UiNumberInput.vue'
import { equalJSON, type ProxyPoolGraph } from '../utils/proxyPoolGraph'
import PageAlert from './PageAlert.vue'
import ModalDialog from './ModalDialog.vue'
import ProxyPoolEditor from './ProxyPoolEditor.vue'
import SubscriptionUserAgent from './SubscriptionUserAgent.vue'
const props=defineProps<{nodeId:number}>()
const pools=ref<NodeProxyPool[]>([]),loading=ref(false),saving=ref(false),open=ref(false),error=ref(''),formError=ref(''),name=ref(''),editorKey=ref(0)
const editing=ref<NodeProxyPool|null>(null),editor=ref<InstanceType<typeof ProxyPoolEditor>|null>(null)
const failure=(e:any)=>e?.response?.data?.message || e?.message || '操作失败。'
const configLoading=ref(false),configReady=ref(false),initialConfig=ref<ProxyPoolGraph>(),runtime=ref<NodeProxyPoolRuntime|null>(null),runtimeLoading=ref(false),runtimeError=ref('')
const syncingID=ref(0),maintenanceMode=ref<'subscription'|'manual'>('manual'),currentSubscription=ref<NodeProxyPoolSubscription|null>(null)
const subscription=ref({url:'',format:'auto' as NodeProxyPoolSubscription['format'],user_agent:'Clash.Meta',auto_sync:false,sync_interval_seconds:86400})
const subscriptionFormats=[{label:'自动识别',value:'auto'},{label:'Zero Base64 JSON',value:'zero'},{label:'Zero JSON',value:'zero-json'},{label:'Clash YAML',value:'clash'},{label:'sing-box JSON',value:'sing-box'},{label:'节点链接（文本 / Base64）',value:'links'}]
let generation=0,editGeneration=0
function resetSubscription(){subscription.value={url:'',format:'auto',user_agent:'Clash.Meta',auto_sync:false,sync_interval_seconds:86400};currentSubscription.value=null;maintenanceMode.value='manual'}
function close() { editGeneration++;open.value=false;initialConfig.value=undefined;runtime.value=null;configReady.value=false;configLoading.value=false;runtimeLoading.value=false;resetSubscription() }

async function load() { const seq=++generation;loading.value=true;error.value='';try { const rows=await fetchNodeProxyPools(props.nodeId);if(seq===generation)pools.value=rows }catch(e){if(seq===generation)error.value=failure(e)}finally{if(seq===generation)loading.value=false} }
async function edit(pool?:NodeProxyPool) {
 close();const seq=++editGeneration
 editing.value=pool||null;name.value=pool?.name||'';formError.value='';runtimeError.value='';editorKey.value++;open.value=true
 if(!pool){configReady.value=true;return}
 configLoading.value=true
 try {
  const detail=await fetchNodeProxyPoolConfig(pool.id)
  if(seq!==editGeneration)return
  editing.value={...pool,...detail.pool};name.value=detail.pool.name;initialConfig.value=detail.config;currentSubscription.value=detail.subscription||null
  if(detail.subscription?.configured){maintenanceMode.value='subscription';subscription.value={url:detail.subscription.url||'',format:detail.subscription.format||'auto',user_agent:detail.subscription.user_agent||'Clash.Meta',auto_sync:detail.subscription.auto_sync,sync_interval_seconds:detail.subscription.sync_interval_seconds||86400}}
  configReady.value=true
 }catch(e){if(seq===editGeneration)formError.value=failure(e)}finally{if(seq===editGeneration)configLoading.value=false}
}
async function readRuntime() {
 if(!editing.value || runtimeLoading.value)return
 const seq=editGeneration,id=editing.value.id
 runtimeLoading.value=true;runtimeError.value='';runtime.value=null
 try { const snapshot=await fetchNodeProxyPoolRuntime(id);if(seq===editGeneration)runtime.value=snapshot }
 catch(e){if(seq===editGeneration)runtimeError.value=failure(e)}finally{if(seq===editGeneration)runtimeLoading.value=false}
}
async function save() {
 const seq=editGeneration
 saving.value=true;formError.value=''
 try {
  if(!name.value)throw new Error('请填写代理池名称。')
  const payload:Record<string,unknown>={node_id:props.nodeId,name:name.value,revision:editing.value?.revision||0}
  if(maintenanceMode.value==='manual'){
   if(!configReady.value || !editor.value)throw new Error('代理表单未就绪。')
   const config=editor.value.build()
   if(!editing.value || !equalJSON(config,initialConfig.value))payload.config=config
  }else{
   if(!subscription.value.url)throw new Error('请填写订阅地址。')
   Object.assign(payload,subscriptionPayload())
  }
  await saveNodeProxyPool(editing.value?.id||0,payload)
  if(seq!==editGeneration)return
  close();await load()
 }catch(e){if(seq===editGeneration)formError.value=failure(e)}finally{saving.value=false}
}
function subscriptionPayload(){return {subscription_url:subscription.value.url,subscription_format:subscription.value.format,subscription_user_agent:subscription.value.user_agent,auto_sync:subscription.value.auto_sync,sync_interval_seconds:subscription.value.sync_interval_seconds}}
async function sync(pool:NodeProxyPool){if(syncingID.value)return;syncingID.value=pool.id;error.value='';try{await syncNodeProxyPool(pool.id,pool.revision);await load()}catch(e){const message=failure(e);await load();error.value=message}finally{syncingID.value=0}}
async function syncEditing(){if(!editing.value||syncingID.value)return;if(!subscription.value.url){formError.value='请填写订阅地址。';return}const id=editing.value.id;syncingID.value=id;formError.value='';try{const saved=await saveNodeProxyPool(id,{node_id:props.nodeId,name:name.value,revision:editing.value.revision,...subscriptionPayload()});await syncNodeProxyPool(id,saved.revision);const detail=await fetchNodeProxyPoolConfig(id);editing.value={...editing.value,...detail.pool};initialConfig.value=detail.config;currentSubscription.value=detail.subscription||null;editorKey.value++;await load()}catch(e){formError.value=failure(e)}finally{syncingID.value=0}}
async function detachSubscription(){if(!editing.value)return;if(!await confirmAction({title:'移除订阅维护',message:'保留当前代理池配置，但不再保存或自动更新此订阅地址。',confirmText:'移除'}))return;saving.value=true;formError.value='';try{const saved=await saveNodeProxyPool(editing.value.id,{node_id:props.nodeId,name:name.value,revision:editing.value.revision,subscription_url:'',auto_sync:false});editing.value={...editing.value,...saved};resetSubscription();maintenanceMode.value='manual';await load()}catch(e){formError.value=failure(e)}finally{saving.value=false}}
async function remove(pool:NodeProxyPool) { if(!await confirmAction({title:'删除共享代理池',message:`删除「${pool.name}」？仍被前置服务引用时不能删除。`,confirmText:'删除',tone:'danger'}))return;try{await deleteNodeProxyPool(pool.id);await load()}catch(e){error.value=failure(e)} }
watch(()=>props.nodeId,()=>{close();pools.value=[];void load()},{immediate:true})
</script>
<style scoped>.pool-subscription-support{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:12px 14px;background:var(--surface);border:1px solid var(--line);border-radius:8px;font-size:12px;line-height:1.6}.pool-subscription-support>div{display:grid;gap:2px}.pool-subscription-support strong{font-weight:600;color:var(--text)}.pool-subscription-support span{color:var(--muted)}.pool-subscription-support a{flex-shrink:0;color:var(--primary);text-decoration:none}.pool-subscription-support a:hover{text-decoration:underline}@media(max-width:480px){.pool-subscription-support{align-items:flex-start;flex-direction:column;gap:8px}}.pool-runtime{margin-top:20px}.pool-hash{overflow-wrap:anywhere;font-size:11px;color:var(--muted)}.pool-runtime textarea{font-family:var(--font-mono);width:100%}.pool-heading{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:12px;border-bottom:1px solid var(--line)}.pool-heading>div:last-child,.pool-mode-actions,.pool-sync-status,.pool-sync-status>div{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.pool-heading p{color:var(--muted);font-size:12px;line-height:1.7}.pool-heading .pool-sync-error{color:var(--danger)}.pool-heading h3{margin:0}.pool-subscription{padding:16px;border:1px solid var(--line);border-radius:12px}.pool-sync-status{justify-content:space-between}.pool-sync-status p{margin:0;color:var(--muted);font-size:12px}@media(max-width:640px){.pool-heading{align-items:stretch;flex-direction:column}}</style>
