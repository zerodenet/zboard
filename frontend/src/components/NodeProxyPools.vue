<template>
 <section class="stack" aria-label="节点共享代理池">
  <header class="pool-heading"><div><h3>共享代理池</h3><p>供本节点的前置端口转发引用。每条入口可独立选择直连落地，或使用这里的代理池；客户端握手和认证始终在落地完成。</p></div><UiButton @click="edit()">创建代理池</UiButton></header>
  <PageAlert v-if="error" tone="danger" title="代理池操作失败">{{error}}</PageAlert>
  <p v-if="loading">正在加载代理池…</p>
  <p v-else-if="!pools.length">尚未配置代理池。前置入口默认直连落地，不需要创建池。</p>
  <article v-for="pool in pools" :key="pool.id" class="pool-heading"><div><strong>{{pool.name}}</strong><p>{{pool.entry_count}} 条前置服务引用</p></div><div><UiButton variant="secondary" @click="edit(pool)">编辑</UiButton><UiButton variant="danger" @click="remove(pool)">删除</UiButton></div></article>
  <ModalDialog :open="open" :title="editing ? '编辑共享代理池' : '创建共享代理池'" :busy="saving" size="xl" @close="open=false">
   <div class="stack"><PageAlert v-if="formError" tone="danger" title="无法保存代理池">{{formError}}</PageAlert>
    <FormField label="代理池名称" required><UiInput v-model.trim="name" maxlength="80" /></FormField>
    <FormField v-if="editing" label="代理配置"><UiSelect v-model="replace" :options="[{label:'保留现有代理和凭据',value:false},{label:'替换代理池配置',value:true}]" /></FormField>
    <p v-if="editing && !replace">凭据加密保存，不回显。可单独修改名称；替换配置将更新所有引用此池的入口。</p>
    <ProxyPoolEditor v-if="!editing || replace" :key="editorKey" ref="editor" />
   </div>
   <template #footer><UiButton variant="secondary" @click="open=false">取消</UiButton><UiButton :loading="saving" @click="save">保存并发布到本节点</UiButton></template>
  </ModalDialog>
 </section>
</template>
<script setup lang="ts">
import {ref,watch} from 'vue'
import {fetchNodeProxyPools,saveNodeProxyPool,deleteNodeProxyPool,type NodeProxyPool} from '../api/client'
import {confirmAction} from '../utils/feedback'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import PageAlert from './PageAlert.vue'
import ModalDialog from './ModalDialog.vue'
import ProxyPoolEditor from './ProxyPoolEditor.vue'
const props=defineProps<{nodeId:number}>()
const pools=ref<NodeProxyPool[]>([]),loading=ref(false),saving=ref(false),open=ref(false),error=ref(''),formError=ref(''),name=ref(''),replace=ref(false),editorKey=ref(0)
const editing=ref<NodeProxyPool|null>(null),editor=ref<InstanceType<typeof ProxyPoolEditor>|null>(null)
const failure=(e:any)=>e?.response?.data?.message || e?.message || '操作失败。'
let generation=0
async function load() { const seq=++generation;loading.value=true;error.value='';try { const rows=await fetchNodeProxyPools(props.nodeId);if(seq===generation)pools.value=rows }catch(e){if(seq===generation)error.value=failure(e)}finally{if(seq===generation)loading.value=false} }
function edit(pool?:NodeProxyPool) { editing.value=pool||null;name.value=pool?.name||'';replace.value=false;formError.value='';editorKey.value++;open.value=true }
async function save() { saving.value=true;formError.value='';try { if(!name.value)throw new Error('请填写代理池名称。'); const payload:Record<string,unknown>={node_id:props.nodeId,name:name.value,revision:editing.value?.revision||0};if(!editing.value||replace.value){if(!editor.value)throw new Error('代理表单未就绪');payload.config=editor.value.build()}await saveNodeProxyPool(editing.value?.id||0,payload);open.value=false;await load() }catch(e){formError.value=failure(e)}finally{saving.value=false} }
async function remove(pool:NodeProxyPool) { if(!await confirmAction({title:'删除共享代理池',message:`删除「${pool.name}」？仍被前置服务引用时不能删除。`,confirmText:'删除',tone:'danger'}))return;try{await deleteNodeProxyPool(pool.id);await load()}catch(e){error.value=failure(e)} }
watch(()=>props.nodeId,()=>{open.value=false;pools.value=[];void load()},{immediate:true})
</script>
<style scoped>.pool-heading{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:12px;border-bottom:1px solid var(--line)}.pool-heading p{color:var(--muted);font-size:12px;line-height:1.7}.pool-heading h3{margin:0}@media(max-width:640px){.pool-heading{align-items:stretch;flex-direction:column}}</style>
