<template>
  <section class="standard-page">
    <PageHeader title="节点组" description="以节点组表达套餐的交付边界。列表仅展示可比较字段，端点成员在编辑器中按需远程搜索。" eyebrow="Delivery Boundary">
      <template #actions><PageRefreshButton label="刷新节点组" :loading="loading" @click="refresh" /><UiButton type="button" @click="openCreate"><UiIcon name="plus" />创建节点组</UiButton></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="节点组已更新" error-title="节点组操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters><WorkbenchFilterBar :active="Boolean(search || enabledFilter)" @clear="clearFilters"><WorkbenchFilterInput v-model="search" label="搜索" placeholder="名称、代码或说明" @apply="applyFilters" /><WorkbenchFilterSelect v-model="enabledFilter" label="启用状态" :options="enabledOptions" @apply="applyFilters" /></WorkbenchFilterBar></template>
      <DataTable v-if="groups.length" caption="节点组列表；成员数量直接展示，完整成员仅在编辑时按需加载" :row-count="total" :min-width="900" table-class="group-table"><thead><tr><th class="table-primary-column">节点组</th><th data-column-priority="2">代码</th><th>状态</th><th class="numeric-column">端点数</th><th class="numeric-column" data-column-priority="3">套餐数</th><th data-column-priority="2">更新时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="group in groups" :key="group.id"><td class="table-primary-column"><div class="cell-title"><strong>{{ group.name }}</strong><span>{{ group.description || '暂无说明' }}</span></div></td><td data-column-priority="2"><code>{{ group.code }}</code></td><td><StatusBadge :tone="group.is_enabled ? 'success' : 'neutral'">{{ group.is_enabled ? '已启用' : '已停用' }}</StatusBadge></td><td class="numeric-column">{{ group.protocol_endpoint_count || 0 }}</td><td class="numeric-column" data-column-priority="3">{{ group.plan_count || 0 }}</td><td data-column-priority="2"><TimeBadge :value="group.updated_at" /></td><td class="table-action-column"><UiButton variant="secondary" size="sm" type="button" :loading="editingID === group.id" :disabled="Boolean(editingID)" @click="openEdit(group)"><UiIcon name="edit" />编辑</UiButton></td></tr></tbody></DataTable>
      <EmptyState v-else icon="nodes" title="没有匹配的节点组" description="调整筛选条件，或创建第一个节点组。"><template #actions><UiButton  type="button" @click="openCreate"><UiIcon name="plus" />创建节点组</UiButton></template></EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <ModalDialog :open="editorOpen" :dirty="editorState.dirty.value" :title="form.id ? '编辑节点组' : '创建节点组'" description="节点组只持有协议端点成员；端点倍率仍由协议端点维护。" size="xl" :busy="saving" @close="closeEditor">
      <form id="node-group-form" ref="formElement" class="group-editor" novalidate @submit.prevent="save">
        <section class="group-members">
          <header><strong>按需搜索成员</strong><p>列表只展示 25 条；批量加入或移除会解析一次最多 10000 个 ID 的服务端筛选快照，不逐页拼接详情。</p></header>
          <FormField v-slot="{ controlAttrs }" label="协议端点成员" name="node-group-endpoints" :error="editorErrors.fields.protocol_endpoint_ids" required full><EndpointMultiLookup v-model="form.protocol_endpoint_ids" v-bind="controlAttrs" /></FormField>
        </section>
        <aside class="group-fields">
          <div v-if="form.id" class="group-editor-meta"><StatusBadge tone="neutral" icon="history">版本 {{ form.revision }}</StatusBadge><TimeBadge :value="form.updated_at" /></div>
          <PageAlert v-if="revisionConflict" tone="warning" title="节点组已在其他会话更新">
            当前草稿基于旧版本，继续覆盖会丢失其他管理员的成员或字段修改。请重新加载最新版本后再编辑。
            <template #actions><UiButton variant="secondary" size="sm" type="button" :loading="editingID === form.id" @click="reloadEditor"><UiIcon name="refresh" />重新加载最新版本</UiButton></template>
          </PageAlert>
          <PageAlert v-if="editorErrors.formError.value" tone="danger" title="无法保存节点组">{{ editorErrors.formError.value }}</PageAlert>
          <FormField v-slot="{ controlAttrs }" label="节点组名称" name="node-group-name" :error="editorErrors.fields.name" required hint="用于运营识别和套餐选择。"><UiInput v-model.trim="form.name" v-bind="controlAttrs" placeholder="例如：香港标准线路" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="代码" name="node-group-code" :error="editorErrors.fields.code" hint="创建时可留空自动生成；编辑时保持稳定。"><UiInput v-model.trim="form.code" v-bind="controlAttrs" :disabled="Boolean(form.id)" placeholder="hong-kong-standard" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="用途说明" name="node-group-description" :error="editorErrors.fields.description" hint="说明该节点组的地域、性能或销售用途。"><UiTextarea v-model.trim="form.description" v-bind="controlAttrs" rows="5" maxlength="255" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="交付状态" name="node-group-enabled" :error="editorErrors.fields.is_enabled"><label class="check-field"><UiCheckbox v-model="form.is_enabled" v-bind="controlAttrs" /><span><strong>启用节点组</strong><br /><small class="field-hint">停用后，新套餐不能再选择该节点组。</small></span></label></FormField>
        </aside>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="node-group-form" type="submit" :loading="saving" :disabled="revisionConflict">保存节点组</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createNodeGroup, fetchNodeGroupDetail, fetchNodeGroupsPage, updateNodeGroup, type NodeGroupSummary } from '../api/client'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import EmptyState from '../components/EmptyState.vue'
import EndpointMultiLookup from '../components/EndpointMultiLookup.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { confirmAction } from '../utils/feedback'
import { preserveAdminReturnTo } from '../utils/navigation'
import { trackAdminTask } from '../utils/taskTracker'
import { collectFieldErrors, isBlank, isSlug, isUtf8LengthInRange } from '../utils/validation'

const route = useRoute()
const router = useRouter()
const search = ref(String(route.query.q || ''))
const enabledFilter = ref(String(route.query.enabled || ''))
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const saving = ref(false), editorOpen = ref(false)
const editingID = ref(0)
const message = ref('')
const formElement = ref<HTMLElement | null>(null)
const originalEndpointIDs = ref<number[]>([])
const revisionConflict = ref(false)
const emptyForm = () => ({ id: 0, name: '', code: '', description: '', is_enabled: true, revision: 0, updated_at: '', protocol_endpoint_ids: [] as number[] })
const form = reactive(emptyForm())
const editorState = useDirtyForm(() => form)
useUnsavedChangesGuard(
  () => editorOpen.value && editorState.dirty.value,
  () => editorState.confirmDiscard({
    title: '放弃节点组修改？',
    message: '离开节点组管理后，尚未保存的字段和端点成员调整将丢失。',
    confirmText: '离开页面',
  }),
)
const editorErrors = useFormErrors()
const nodeGroupFieldMap: Record<string, string> = { name: 'name', code: 'code', description: 'description', is_enabled: 'is_enabled', protocol_endpoint_ids: 'protocol_endpoint_ids' }
for (const [source, field] of [
  [() => form.name, 'name'], [() => form.code, 'code'], [() => form.description, 'description'],
  [() => form.is_enabled, 'is_enabled'], [() => [...form.protocol_endpoint_ids], 'protocol_endpoint_ids'],
] as Array<[() => unknown, string]>) watch(source, () => editorErrors.clear(field))
const enabledOptions = [{ label: '全部状态', value: '' }, { label: '已启用', value: 'true' }, { label: '已停用', value: 'false' }]
const { items: groups, total, loading, refreshing, error, load: refresh } = useRemoteTable<any>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchNodeGroupsPage({ q: search.value || undefined, enabled: enabledFilter.value === '' ? undefined : enabledFilter.value === 'true', offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '节点组加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

function generatedGroupCode(name: string) { const slug = name.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 40); return slug || `group-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}` }
async function syncURL(replace = false) { const page = Math.floor(offset.value / limit.value) + 1; const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(search.value ? { q: search.value } : {}), ...(enabledFilter.value ? { enabled: enabledFilter.value } : {}), ...(page > 1 ? { page: String(page) } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}) } }; await (replace ? router.replace(location) : router.push(location)) }
async function applyFilters() { offset.value = 0; await syncURL(); await refresh() }
async function clearFilters() { search.value = ''; enabledFilter.value = ''; await applyFilters() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await refresh() }
function openCreate() { Object.assign(form, emptyForm()); originalEndpointIDs.value = []; revisionConflict.value = false; editorState.markClean(); editorErrors.clear(); editorOpen.value = true }
async function openEdit(group: NodeGroupSummary) {
  if (editingID.value) return
  editingID.value = group.id
  error.value = ''
  try {
    const detail = await fetchNodeGroupDetail(group.id)
    Object.assign(form, {
      id: detail.id,
      name: detail.name,
      code: detail.code,
      description: detail.description || '',
      is_enabled: Boolean(detail.is_enabled),
      revision: detail.revision,
      updated_at: detail.updated_at,
      protocol_endpoint_ids: [...detail.protocol_endpoint_ids],
    })
    originalEndpointIDs.value = [...detail.protocol_endpoint_ids]
    revisionConflict.value = false
    editorState.markClean()
    editorErrors.clear()
    editorOpen.value = true
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '节点组详情加载失败，请稍后重试。'
  } finally {
    editingID.value = 0
  }
}
async function reloadEditor() {
  if (!form.id || editingID.value) return
  editingID.value = form.id
  try {
    const detail = await fetchNodeGroupDetail(form.id)
    Object.assign(form, {
      id: detail.id,
      name: detail.name,
      code: detail.code,
      description: detail.description || '',
      is_enabled: Boolean(detail.is_enabled),
      revision: detail.revision,
      updated_at: detail.updated_at,
      protocol_endpoint_ids: [...detail.protocol_endpoint_ids],
    })
    originalEndpointIDs.value = [...detail.protocol_endpoint_ids]
    revisionConflict.value = false
    editorErrors.clear()
    editorState.markClean()
  } catch (cause: any) {
    editorErrors.formError.value = cause?.response?.data?.message || '节点组最新版本加载失败，请稍后重试。'
  } finally {
    editingID.value = 0
  }
}
function closeEditor() { if (!saving.value) editorOpen.value = false }
async function save() {
	form.name = form.name.trim()
	form.code = form.code.trim().toLowerCase()
	form.description = form.description.trim()
	const valid = await editorErrors.applyValidation(collectFieldErrors({
		name: !isUtf8LengthInRange(form.name, 1, 80, true) && '节点组名称需包含 1 到 80 个 UTF-8 字节。',
		code: !isBlank(form.code) && !isSlug(form.code, 80) && '代码只能包含小写字母、数字和单个连字符，且不能超过 80 个 UTF-8 字节。',
		description: !isUtf8LengthInRange(form.description, 0, 255) && '用途说明不能超过 255 个 UTF-8 字节。',
		protocol_endpoint_ids: form.is_enabled && !form.protocol_endpoint_ids.length && '启用的节点组至少需要选择一个协议端点。',
	}), formElement, '请更正标记字段后再保存节点组。')
	if (!valid) return
	const nextEndpointIDs = new Set(form.protocol_endpoint_ids)
	const removedEndpointCount = form.id ? originalEndpointIDs.value.filter(id => !nextEndpointIDs.has(id)).length : 0
	if (removedEndpointCount > 0 && !await confirmAction({
		title: `从节点组移除 ${removedEndpointCount} 个端点？`,
		message: '保存后，引用该节点组的套餐和订阅将立即停止交付这些端点，并触发相关凭证与节点配置对齐。',
		confirmText: '确认保存变更',
		tone: 'danger',
	})) return
	saving.value = true; error.value = ''; message.value = ''
	try {
		const payload = { name: form.name, code: form.code || generatedGroupCode(form.name), description: form.description, is_enabled: form.is_enabled, protocol_endpoint_ids: form.protocol_endpoint_ids, ...(form.id ? { expected_revision: form.revision } : {}) }
		const result = form.id ? await updateNodeGroup(form.id, payload) : await createNodeGroup(payload)
		if (result.reconcile_task) trackAdminTask(result.reconcile_task)
		editorOpen.value = false
		message.value = result.reconcile_task ? `节点组已保存，凭证与节点配置对齐任务 #${result.reconcile_task.id} 已启动。` : '节点组已保存。'
		await refresh()
	}
	catch (e: any) {
		if (e?.response?.status === 409 || e?.response?.status === 428) {
			revisionConflict.value = true
			editorErrors.formError.value = '服务器版本已变化。请重新加载当前节点组，确认最新成员和字段后再保存。'
		} else {
			await editorErrors.applyApiError(e, '节点组保存失败，请检查表单内容。', formElement, nodeGroupFieldMap)
		}
	}
  finally { saving.value = false }
}
watch(() => route.fullPath, async () => { const nextSearch = String(route.query.q || ''), nextEnabled = String(route.query.enabled || ''); const rawLimit = Number(route.query.limit), nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50, nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit; if (nextSearch !== search.value || nextEnabled !== enabledFilter.value || nextLimit !== limit.value || nextOffset !== offset.value) { search.value = nextSearch; enabledFilter.value = nextEnabled; limit.value = nextLimit; offset.value = nextOffset; await refresh() } })
onMounted(refresh)
</script>

<style scoped>
:deep(.group-table code) { color: var(--code-text); background: var(--code-soft); padding: 3px 6px; border-radius: 5px; }.group-editor { display: grid; grid-template-columns: minmax(0,1.3fr) minmax(320px,.7fr); min-height: 520px; }.group-members,.group-fields { min-width: 0; padding: 20px; }.group-members { border-right: 1px solid var(--line); background: var(--surface-soft); }.group-members>header { margin-bottom: 14px; }.group-members>header strong { font-size: 13px; }.group-members>header p { margin: 4px 0 0; color: var(--muted); font-size: 10px; }.group-fields { display: grid; align-content: start; gap: 14px; }.group-editor-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }.check-field { min-height: var(--control-height); align-items: center; }
@media(max-width:850px){.group-editor{grid-template-columns:1fr}.group-members{border-right:0;border-bottom:1px solid var(--line)}}
</style>
