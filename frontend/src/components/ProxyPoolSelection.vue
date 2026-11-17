<template>
  <section class="stack pool-selection" aria-label="服务器代理池">
    <div class="pool-selection-actions"><UiButton variant="secondary" @click="editor?.openCreate()">新建代理池</UiButton><UiButton variant="ghost" :disabled="loading" @click="resource.load">刷新已有池</UiButton></div>
    <p>新建一份独立配置，或引用已有池；前置服务仍按各自的转发路径选用。</p>
    <PageAlert v-if="error" tone="danger" title="代理池列表未加载">{{ error }}</PageAlert>
    <p v-if="loading && !loaded" role="status">正在加载代理池…</p>
    <label v-for="pool in pools" :key="pool.id" class="pool-selection-option"><UiCheckbox :model-value="modelValue.includes(pool.id)" @update:model-value="select(pool.id, Boolean($event))" /><span><strong>{{ pool.name }}</strong><small>{{ pool.nodes?.map(node => node.name).join('、') || '尚未引用' }}</small></span></label>
    <p v-if="loaded && !pools.length">暂无已有代理池，可直接新建。</p>
    <NodeProxyPools ref="editor" :show-list="false" @saved="created" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchNodeProxyPools, type NodeProxyPool } from '../api/client'
import { useRemoteResource } from '../composables/useRemoteResource'
import { normalizeApiErrorMessage } from '../utils/apiError'
import NodeProxyPools from './NodeProxyPools.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import PageAlert from './PageAlert.vue'
const props = defineProps<{ modelValue: number[] }>()
const emit = defineEmits<{ 'update:modelValue': [number[]] }>()
const editor=ref<InstanceType<typeof NodeProxyPools>|null>(null)
const resource=useRemoteResource<NodeProxyPool[]>({initial:()=>[],fetch:()=>fetchNodeProxyPools(),errorMessage:cause=>normalizeApiErrorMessage(cause,'暂时无法读取共享代理池。')})
const {data:pools,loading,loaded,error}=resource
function select(id:number,checked:boolean){emit('update:modelValue',checked?[...new Set([...props.modelValue,id])]:props.modelValue.filter(value=>value!==id))}
async function created(pool:NodeProxyPool){select(pool.id,true);await resource.load()}
onMounted(resource.load)
</script>
<style scoped>
.pool-selection { margin-top: 16px; min-width: 0; }
.pool-selection-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.pool-selection p,.pool-selection-option small { margin: 0; font-size: 12px; line-height: 1.6; color: var(--muted-foreground); }
.pool-selection-option { display: flex; gap: 10px; align-items: center; min-height: 40px; }
.pool-selection-option span { display: grid; min-width: 0; gap: 4px; overflow-wrap: anywhere; }
.pool-selection-option strong { font-size: 13px; font-weight: 500; }
</style>
