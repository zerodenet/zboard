<template>
 <section class="stack" aria-label="节点共享代理池">
  <header class="pool-heading"><div><h3>共享代理池</h3><p>供本节点的前置端口转发引用。每条入口可独立选择直连落地，或使用这里的代理池；客户端握手和认证始终在落地完成。</p></div><UiButton @click="edit()">创建代理池</UiButton></header>
  <PageAlert v-if="error" tone="danger" title="代理池操作失败">{{error}}</PageAlert>
  <p v-if="loading">正在加载代理池…</p>
  <p v-else-if="!pools.length">尚未配置代理池。前置入口默认直连落地，不需要创建池。</p>
  <article v-for="pool in pools" :key="pool.id" class="pool-heading"><div><strong>{{pool.name}}</strong><p>{{pool.entry_count}} 条前置服务引用</p></div><div><UiButton variant="secondary" @click="edit(pool)">编辑</UiButton><UiButton variant="danger" @click="remove(pool)">删除</UiButton></div></article>
  <ModalDialog :open="open" :title="editing ? '编辑共享代理池' : '创建共享代理池'" :busy="saving" size="xl" @close="close">
   <div class="stack"><PageAlert v-if="formError" tone="danger" title="无法保存代理池">{{formError}}</PageAlert>
    <FormField label="代理池名称" required><UiInput v-model.trim="name" maxlength="80" /></FormField>
    <p v-if="configLoading">正在读取现有代理和凭据…</p>
    <ProxyPoolEditor v-if="open && !configLoading && configReady" :key="editorKey" ref="editor" :config="initialConfig" />
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
   <template #footer><UiButton variant="secondary" @click="close">取消</UiButton><UiButton :loading="saving" :disabled="configLoading || !configReady" @click="save">保存并发布到本节点</UiButton></template>
  </ModalDialog>
 </section>
</template>
<script setup lang="ts">
import {ref,watch} from 'vue'
import {fetchNodeProxyPools,fetchNodeProxyPoolConfig,fetchNodeProxyPoolRuntime,saveNodeProxyPool,deleteNodeProxyPool,type NodeProxyPool,type NodeProxyPoolRuntime} from '../api/client'
import {confirmAction} from '../utils/feedback'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import UiTextarea from './UiTextarea.vue'
import { equalJSON, type ProxyPoolGraph } from '../utils/proxyPoolGraph'
import PageAlert from './PageAlert.vue'
import ModalDialog from './ModalDialog.vue'
import ProxyPoolEditor from './ProxyPoolEditor.vue'
const props=defineProps<{nodeId:number}>()
const pools=ref<NodeProxyPool[]>([]),loading=ref(false),saving=ref(false),open=ref(false),error=ref(''),formError=ref(''),name=ref(''),editorKey=ref(0)
const editing=ref<NodeProxyPool|null>(null),editor=ref<InstanceType<typeof ProxyPoolEditor>|null>(null)
const failure=(e:any)=>e?.response?.data?.message || e?.message || '操作失败。'
const configLoading=ref(false),configReady=ref(false),initialConfig=ref<ProxyPoolGraph>(),runtime=ref<NodeProxyPoolRuntime|null>(null),runtimeLoading=ref(false),runtimeError=ref('')
let generation=0,editGeneration=0
function close() { editGeneration++;open.value=false;initialConfig.value=undefined;runtime.value=null;configReady.value=false;configLoading.value=false;runtimeLoading.value=false }

async function load() { const seq=++generation;loading.value=true;error.value='';try { const rows=await fetchNodeProxyPools(props.nodeId);if(seq===generation)pools.value=rows }catch(e){if(seq===generation)error.value=failure(e)}finally{if(seq===generation)loading.value=false} }
async function edit(pool?:NodeProxyPool) {
 close();const seq=++editGeneration
 editing.value=pool||null;name.value=pool?.name||'';formError.value='';runtimeError.value='';editorKey.value++;open.value=true
 if(!pool){configReady.value=true;return}
 configLoading.value=true
 try {
  const detail=await fetchNodeProxyPoolConfig(pool.id)
  if(seq!==editGeneration)return
  editing.value={...pool,...detail.pool};name.value=detail.pool.name;initialConfig.value=detail.config;configReady.value=true
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
  if(!configReady.value || !editor.value)throw new Error('代理表单未就绪。')
  const config=editor.value.build()
  const payload:Record<string,unknown>={node_id:props.nodeId,name:name.value,revision:editing.value?.revision||0}
  if(!editing.value || !equalJSON(config,initialConfig.value))payload.config=config
  await saveNodeProxyPool(editing.value?.id||0,payload)
  if(seq!==editGeneration)return
  close();await load()
 }catch(e){if(seq===editGeneration)formError.value=failure(e)}finally{saving.value=false}
}
async function remove(pool:NodeProxyPool) { if(!await confirmAction({title:'删除共享代理池',message:`删除「${pool.name}」？仍被前置服务引用时不能删除。`,confirmText:'删除',tone:'danger'}))return;try{await deleteNodeProxyPool(pool.id);await load()}catch(e){error.value=failure(e)} }
watch(()=>props.nodeId,()=>{close();pools.value=[];void load()},{immediate:true})
</script>
<style scoped>.pool-runtime{margin-top:20px}.pool-hash{overflow-wrap:anywhere;font-size:11px;color:var(--muted)}.pool-runtime textarea{font-family:var(--font-mono);width:100%}.pool-heading{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:12px;border-bottom:1px solid var(--line)}.pool-heading p{color:var(--muted);font-size:12px;line-height:1.7}.pool-heading h3{margin:0}@media(max-width:640px){.pool-heading{align-items:stretch;flex-direction:column}}</style>
