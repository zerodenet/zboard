<template>
  <section>
    <PageHeader title="节点组" description="节点组隔离套餐可见的协议端点；套餐只选择一个节点组，不直接修改端点成员。" eyebrow="Delivery boundary">
      <template #actions>
        <button class="button button-secondary" type="button" :disabled="loading" @click="refresh"><UiIcon name="refresh" />刷新</button>
        <button class="button" type="button" @click="openCreate"><UiIcon name="plus" />创建节点组</button>
      </template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div v-if="groups.length" class="group-grid">
      <article v-for="group in groups" :key="group.id" class="panel group-card">
        <header class="group-header"><div class="group-title"><span><UiIcon name="nodes" /></span><div><div class="title-line"><h2>{{ group.name }}</h2><StatusBadge :tone="group.is_enabled ? 'success' : 'neutral'">{{ group.is_enabled ? '启用' : '停用' }}</StatusBadge></div><code>{{ group.code }}</code></div></div><button class="button button-secondary button-sm" type="button" @click="openEdit(group)"><UiIcon name="edit" />编辑</button></header>
        <p class="group-description">{{ group.description || '暂无说明' }}</p>
        <div class="group-summary"><span><strong>{{ group.protocol_endpoint_ids?.length || 0 }}</strong> 个协议端点</span><span><strong>{{ planCount(group.id) }}</strong> 个套餐</span></div>
        <div class="endpoint-list">
          <div v-for="endpointID in group.protocol_endpoint_ids" :key="endpointID"><span class="endpoint-dot" :class="{ active: endpointByID(endpointID)?.is_active }"></span><div><strong>{{ endpointByID(endpointID)?.name || `协议端点 #${endpointID}` }}</strong><small>{{ endpointDescription(endpointByID(endpointID)) }}</small></div><em>{{ formatMultiplier(endpointByID(endpointID)?.multiplier_milli) }}</em></div>
          <p v-if="!group.protocol_endpoint_ids?.length" class="empty-members">尚未加入协议端点；启用的套餐不能使用空节点组。</p>
        </div>
      </article>
    </div>
    <article v-else class="panel"><EmptyState icon="nodes" title="还没有节点组" description="创建节点组并选择协议端点后，套餐才能建立稳定的交付边界。"><template #actions><button class="button" type="button" @click="openCreate"><UiIcon name="plus" />创建节点组</button></template></EmptyState></article>

    <ModalDialog :open="editorOpen" :title="form.id ? '编辑节点组' : '创建节点组'" description="成员关系只在这里维护；修改会同时影响所有引用该节点组的套餐。" size="lg" :busy="saving" @close="editorOpen = false">
      <form id="node-group-form" class="stack" @submit.prevent="save">
        <div class="form-grid">
          <label class="field"><span>名称</span><input v-model.trim="form.name" required placeholder="标准节点组" /></label>
          <label class="field"><span>代码</span><input v-model.trim="form.code" required pattern="[a-zA-Z0-9_-]+" placeholder="standard" /></label>
          <label class="field field-full"><span>说明</span><textarea v-model.trim="form.description" rows="3" placeholder="适用套餐、区域或能力边界"></textarea></label>
          <label class="check-field field-full"><input v-model="form.is_enabled" type="checkbox" /><span>启用节点组</span></label>
        </div>
        <div class="field"><span>协议端点</span><div class="endpoint-picker"><label v-for="endpoint in activeEndpoints" :key="endpoint.id" class="endpoint-option"><input v-model="form.protocol_endpoint_ids" type="checkbox" :value="endpoint.id" /><span><strong>{{ endpoint.name }}</strong><small>{{ endpoint.protocol }} · {{ endpoint.address }}:{{ endpoint.public_port || endpoint.port }} · {{ nodeName(endpoint.node_id) }}</small></span><em>{{ formatMultiplier(endpoint.multiplier_milli) }}</em></label><p v-if="!activeEndpoints.length" class="empty-members">没有启用的协议端点，请先在“协议服务”页面创建并启用。</p></div><small class="field-hint">一个协议端点可以属于多个节点组，从而复用于不同套餐组合。</small></div>
      </form>
      <template #footer><button class="button button-secondary" type="button" :disabled="saving" @click="editorOpen = false">取消</button><button class="button" form="node-group-form" type="submit" :disabled="saving">{{ saving ? '保存中…' : '保存节点组' }}</button></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { createNodeGroup, fetchNodeGroups, fetchNodes, fetchPlansWithOptions, fetchProtocolEndpoints, updateNodeGroup } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'

const groups = ref<any[]>([]), endpoints = ref<any[]>([]), nodes = ref<any[]>([]), plans = ref<any[]>([])
const loading = ref(false), saving = ref(false), editorOpen = ref(false)
const error = ref(''), message = ref('')
const emptyForm = () => ({ id: 0, name: '', code: '', description: '', is_enabled: true, protocol_endpoint_ids: [] as number[] })
const form = reactive(emptyForm())
const activeEndpoints = computed(() => endpoints.value.filter(endpoint => endpoint.is_active))
function endpointByID(id: number) { return endpoints.value.find(endpoint => endpoint.id === id) }
function nodeName(id?: number) { return nodes.value.find(node => node.id === id)?.name || (id ? `VPS #${id}` : '未知 VPS') }
function endpointDescription(endpoint: any) { return endpoint ? `${endpoint.protocol} · ${nodeName(endpoint.node_id)} · ${endpoint.address}:${endpoint.public_port || endpoint.port}` : '端点已不可用' }
function formatMultiplier(value?: number) { return `${Number(value || 1000) / 1000}×` }
function planCount(groupID: number) { return plans.value.filter(plan => plan.node_group_id === groupID).length }
async function refresh() { loading.value = true; error.value = ''; try { const [groupItems, endpointItems, nodeItems, planItems] = await Promise.all([fetchNodeGroups(), fetchProtocolEndpoints(), fetchNodes(), fetchPlansWithOptions({ includeInactive: true })]); groups.value = groupItems; endpoints.value = endpointItems; nodes.value = nodeItems; plans.value = planItems } catch (e: any) { error.value = e?.response?.data?.message || '节点组加载失败。' } finally { loading.value = false } }
function openCreate() { Object.assign(form, emptyForm()); editorOpen.value = true }
function openEdit(group: any) { Object.assign(form, { id: group.id, name: group.name, code: group.code, description: group.description || '', is_enabled: Boolean(group.is_enabled), protocol_endpoint_ids: [...(group.protocol_endpoint_ids || [])] }); editorOpen.value = true }
async function save() { saving.value = true; error.value = ''; message.value = ''; try { const payload = { name: form.name, code: form.code, description: form.description, is_enabled: form.is_enabled, protocol_endpoint_ids: form.protocol_endpoint_ids }; if (form.id) await updateNodeGroup(form.id, payload); else await createNodeGroup(payload); editorOpen.value = false; message.value = '节点组及协议成员已保存。'; await refresh() } catch (e: any) { error.value = e?.response?.data?.message || '节点组保存失败。' } finally { saving.value = false } }
onMounted(refresh)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.group-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }.group-card { overflow: hidden; }.group-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 18px 18px 12px; }.group-title { display: flex; gap: 11px; }.group-title > span { width: 40px; height: 40px; display: grid; place-items: center; border-radius: 10px; color: var(--primary); background: var(--primary-soft); font-size: 19px; }.title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }.title-line h2 { margin: 0; font-size: 16px; }.group-title code { display: block; margin-top: 4px; color: var(--muted); font-size: 10px; }.group-description { min-height: 20px; margin: 0; padding: 0 18px 14px; color: var(--muted); font-size: 11px; }.group-summary { display: flex; gap: 24px; padding: 12px 18px; border-block: 1px solid var(--line); background: var(--surface-soft); color: var(--muted); font-size: 11px; }.group-summary strong { color: var(--text-strong); font-size: 15px; }.endpoint-list { display: grid; gap: 9px; padding: 14px 18px 18px; }.endpoint-list > div { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 9px; }.endpoint-list strong, .endpoint-list small { display: block; }.endpoint-list strong { font-size: 11px; }.endpoint-list small { margin-top: 2px; color: var(--muted); font-size: 10px; overflow-wrap: anywhere; }.endpoint-list em, .endpoint-option em { color: var(--primary); font-size: 11px; font-style: normal; font-weight: 700; }.endpoint-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--subtle); }.endpoint-dot.active { background: var(--success); }.empty-members { margin: 0; color: var(--muted); font-size: 11px; }.endpoint-picker { display: grid; gap: 8px; max-height: 360px; padding: 10px; overflow-y: auto; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.endpoint-option { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 9px; padding: 10px; border-radius: 8px; background: var(--surface); }.endpoint-option strong, .endpoint-option small { display: block; }.endpoint-option strong { font-size: 11px; }.endpoint-option small { margin-top: 2px; color: var(--muted); font-size: 10px; }
@media (max-width: 850px) { .group-grid { grid-template-columns: 1fr; } }
</style>
