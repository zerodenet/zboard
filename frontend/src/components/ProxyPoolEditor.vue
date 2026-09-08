<template>
 <section class="stack" aria-label="共享代理池配置">
  <FormField label="配置方式"><UiSelect v-model="mode" :options="[{label:'URLTest 自动测速池',value:'form'},{label:'Raw 覆盖',value:'raw'}]" /></FormField>
  <template v-if="mode === 'form'">
   <FormField label="测速地址"><UiInput v-model.trim="url" placeholder="留空使用 Zero 默认值" /></FormField>
   <FormField label="测速间隔（秒）"><UiInput v-model.number="interval" type="number" min="1" /></FormField>
   <FormField label="切换容差（毫秒）"><UiInput v-model.number="tolerance" type="number" min="0" /></FormField>
   <article v-for="(id,index) in members" :key="id" class="stack">
    <header class="pool-member-heading"><strong>代理 {{ index+1 }}</strong><UiButton variant="ghost" :disabled="members.length === 1" @click="members = members.filter(item => item !== id); editors.delete(id)">移除</UiButton></header>
    <NetworkEntryPathEditor :ref="value => bindEditor(id,value)" />
   </article>
   <UiButton variant="secondary" @click="members.push(++sequence)">添加代理节点</UiButton>
  </template>
  <FormField v-else label="完整代理池 Raw" hint="填写 outbounds、outbound_groups、target；完整覆盖表单，保留 Zero 支持的参数。" full><UiTextarea v-model="raw" rows="18" /></FormField>
 </section>
</template>
<script setup lang="ts">
import {ref} from 'vue'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'
import NetworkEntryPathEditor from './NetworkEntryPathEditor.vue'
import {parseRawPath} from '../utils/networkEntryPath'
import {buildSharedProxyPool} from '../utils/sharedProxyPool'
const mode=ref('form'),url=ref(''),interval=ref(300),tolerance=ref(50),raw=ref('')
let sequence=1
const members=ref([1]),editors=new Map<number,{build:()=>Record<string,any>}>()
function bindEditor(id:number,value:any) { if(value) editors.set(id,value);else editors.delete(id) }
function build() { return mode.value === 'raw' ? parseRawPath(raw.value) : buildSharedProxyPool(members.value.map(id=>{ const editor=editors.get(id);if(!editor) throw new Error('代理表单尚未就绪。');return editor.build() }),url.value,interval.value,tolerance.value) }
defineExpose({build})
</script>
<style scoped>.pool-member-heading{display:flex;align-items:center;justify-content:space-between}</style>
