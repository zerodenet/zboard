<template>
 <section class="stack" aria-label="共享代理池配置">
  <div class="pool-actions"><UiButton variant="secondary" :disabled="mode === 'form'" @click="applyRaw">覆盖并转为表单</UiButton><UiButton variant="secondary" @click="showRaw">合并表单并查看 RAW</UiButton></div>
  <PageAlert v-if="error" tone="danger" title="配置未应用">{{ error }}</PageAlert>
  <div v-show="mode === 'form'" class="stack">
   <p class="pool-hint">手动编辑代理、分组和最终出口，保存时合并成一份 Zero JSON。修改标识会同步更新引用；高级参数保持原样。</p>
   <details v-for="member in members" :key="member.key" class="pool-item" :open="members.length === 1">
    <summary>{{ member.value.tag }} · {{ member.summary || member.value.protocol.type }}</summary>
    <div class="stack pool-item-body">
    <header class="pool-member-heading"><FormField label="代理标识"><UiInput :model-value="member.value.tag" aria-label="代理标识" @change="rename(member.value, $event.target.value)" /></FormField><UiButton variant="ghost" @click="removeMember(member.key)">移除代理</UiButton></header>
    <NetworkEntryPathEditor :ref="value => bindEditor(member.key,value)" :outbound="member.value" @summary="member.summary = $event" />
    </div>
   </details>
   <UiButton variant="secondary" @click="addMember">添加代理节点</UiButton>
   <details v-for="group in groups" :key="group.key" class="pool-item">
    <summary>{{ group.value.tag }} · {{ groupTypes.find(item => item.value === group.value.type)?.label }} · {{ group.value[memberKey(group.value)].length }} 个成员</summary>
    <div class="stack pool-item-body">
    <header class="pool-member-heading"><FormField label="分组标识"><UiInput :model-value="group.value.tag" aria-label="分组标识" @change="rename(group.value, $event.target.value)" /></FormField><UiButton variant="ghost" @click="groups = groups.filter(item => item.key !== group.key)">移除分组</UiButton></header>
    <FormField label="分组类型"><UiSelect :model-value="group.value.type" :options="groupTypes" @update:model-value="changeGroupType(group.value, $event)" /></FormField>
    <FormField label="成员及顺序" hint="每行一个代理或分组标识；relay 按此顺序连接。"><UiTextarea :model-value="group.value[memberKey(group.value)].join('\n')" aria-label="分组成员" rows="4" @update:model-value="group.value[memberKey(group.value)] = $event.split('\n').map((s: string) => s.trim()).filter(Boolean)" /></FormField>
    <template v-if="group.value.type === 'url_test'">
     <FormField label="测速地址"><UiInput v-model="group.value.url" placeholder="留空使用 Zero 默认值" /></FormField>
     <FormField label="测速间隔（秒）"><UiInput v-model.number="group.value.interval_seconds" type="number" min="1" /></FormField>
     <FormField label="切换容差（毫秒）"><UiInput v-model.number="group.value.tolerance_ms" type="number" min="0" /></FormField>
    </template>
    <template v-if="group.value.type === 'selector'">
     <FormField label="默认成员"><UiSelect :model-value="group.value.default || ''" :options="memberOptions(group.value)" @update:model-value="setOptional(group.value, 'default', $event)" /></FormField>
     <FormField label="选中成员"><UiSelect :model-value="group.value.selected || ''" :options="memberOptions(group.value)" @update:model-value="setOptional(group.value, 'selected', $event)" /></FormField>
    </template>
    </div>
   </details>
   <UiButton variant="secondary" @click="addGroup">添加分组</UiButton>
   <FormField label="最终出口 target" required><UiSelect v-model="target" :options="tagOptions" /></FormField>
  </div>
  <div v-if="mode === 'raw'" class="stack">
   <FormField label="完整代理池 RAW" hint="仅接受 Zero JSON：outbounds、outbound_groups、target。这里完整覆盖整个池；点击“覆盖并转为表单”回填代理和分组，或直接保存。" full><UiTextarea v-model="raw" aria-label="完整代理池 RAW" rows="22" spellcheck="false" /></FormField>
   <UiButton variant="ghost" @click="mode = 'form'; error = ''">放弃 RAW 修改，返回表单</UiButton>
  </div>
 </section>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'
import PageAlert from './PageAlert.vue'
import NetworkEntryPathEditor from './NetworkEntryPathEditor.vue'
import { cloneJSON, parseProxyPoolGraph, validateProxyPoolGraph, type JSONObject, type ProxyPoolGraph } from '../utils/proxyPoolGraph'
const props = defineProps<{ config?: ProxyPoolGraph }>()
const mode = ref('form'), raw = ref(''), error = ref(''), target = ref('shared')
let sequence = 0
const members = ref<{key:number; value:JSONObject; summary?:string}[]>([]), groups = ref<{key:number; value:JSONObject}[]>([])
const editors = new Map<number, {build:(validate?:boolean)=>JSONObject}>()
const memberKey = (group:JSONObject) => group.type === 'relay' ? 'proxies' : 'outbounds'
const groupTypes = [{label:'自动测速',value:'url_test'},{label:'手动选择',value:'selector'},{label:'多跳链路',value:'relay'}]
const tagOptions = computed(() => [...members.value, ...groups.value].map(item => ({label:item.value.tag,value:item.value.tag})))
const memberOptions = (group:JSONObject) => [{label:'未指定',value:''}, ...group[memberKey(group)].map((tag:string) => ({label:tag,value:tag}))]
function setOptional(value:JSONObject,key:string,input:any) { if(input === '' || input == null) delete value[key]; else value[key] = input }
function bindEditor(id:number,value:any) { if(value) editors.set(id,value); else editors.delete(id) }
function uniqueTag(prefix:string) { let i=1; while(tagOptions.value.some(o=>o.value===`${prefix}-${i}`))i++;return `${prefix}-${i}` }
function hydrate(graph:ProxyPoolGraph) {
 const copy = cloneJSON(graph)
 members.value = copy.outbounds.map(value => ({key:++sequence,value}))
 groups.value = (copy.outbound_groups || []).map(value => ({key:++sequence,value}))
 target.value = copy.target
}
function addMember() {
 const tag = uniqueTag('proxy')
 members.value.push({key:++sequence,value:{tag,protocol:{type:'shadowsocks',server:'',port:443,cipher:'chacha20-ietf-poly1305',password:''}}})
 const shared = groups.value.find(g=>g.value.tag === 'shared')
 if(shared) shared.value[memberKey(shared.value)].push(tag)
 if(!target.value) target.value=tag
}
function addGroup() { groups.value.push({key:++sequence,value:{tag:uniqueTag('group'),type:'selector',outbounds:members.value.map(m=>m.value.tag)}}) }
function removeMember(key:number) { members.value=members.value.filter(m=>m.key!==key);editors.delete(key) }
function rename(value:JSONObject,name:string) {
 const previous=value.tag, next=name.trim()
 if (!next || tagOptions.value.some(option=>option.value===next && next!==previous)) { error.value='标识不能为空或重复。';return }
 value.tag=next
 for(const {value:group} of groups.value) {
  group[memberKey(group)]=group[memberKey(group)].map((tag:string)=>tag===previous?next:tag)
  for(const key of ['default','selected'])if(group[key]===previous)group[key]=next
 }
 if(target.value===previous)target.value=next
 error.value=''
}
function changeGroupType(group:JSONObject,type:string) {
 const previousKey=memberKey(group), values=group[previousKey]
 group.type=type
 if(previousKey!==memberKey(group))delete group[previousKey]
 group[memberKey(group)]=values
 if(type!=='selector'){delete group.default;delete group.selected}
 if(type!=='url_test'){delete group.url;delete group.interval_seconds;delete group.tolerance_ms}
}
function formGraph(validate=true):ProxyPoolGraph {
 const graph={outbounds:members.value.map(member=>{
  const editor=editors.get(member.key)
  if(!editor)throw new Error('代理表单尚未就绪。')
  return {...editor.build(validate),tag:member.value.tag}
 }),outbound_groups:cloneJSON(groups.value.map(g=>g.value)),target:target.value}
 for(const group of graph.outbound_groups)if(group.type==='url_test' && group.url==='')delete group.url
 if(validate)validateProxyPoolGraph(graph)
 return graph
}
function showRaw() { if(mode.value==='raw')return;try { raw.value=JSON.stringify(formGraph(false),null,2);mode.value='raw';error.value='' }catch(e){error.value=(e as Error).message} }
function applyRaw() { try { const graph=parseProxyPoolGraph(raw.value);hydrate(graph);mode.value='form';error.value='' }catch(e){error.value=(e as Error).message} }
function build() { return mode.value==='raw' ? parseProxyPoolGraph(raw.value) : formGraph() }
if(props.config)hydrate(props.config)
else { groups.value=[{key:++sequence,value:{tag:'shared',type:'url_test',outbounds:[],interval_seconds:300,tolerance_ms:50}}];addMember() }
defineExpose({build})
</script>
<style scoped>
.pool-member-heading,.pool-actions{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap}.pool-item{border:1px solid var(--line);border-radius:12px;padding:16px}.pool-item summary{cursor:pointer;font-weight:600;overflow-wrap:anywhere}.pool-item-body{padding-top:16px}.pool-hint{color:var(--muted);font-size:13px;line-height:1.7}textarea{font-family:var(--font-mono);width:100%}
</style>
