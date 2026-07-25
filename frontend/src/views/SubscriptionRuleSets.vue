<template>
  <section class="standard-page">
    <PageHeader title="规则集" description="作为订阅模板的可复用远端规则来源；模板只选择规则集并决定命中策略，来源修改会同步影响所有引用模板。" eyebrow="订阅模板">
      <template #actions>
        <PageRefreshButton label="刷新规则集" :loading="loading" @click="load" />
        <UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建规则集</UiButton>
      </template>
    </PageHeader>

    <SubscriptionTemplateSectionNav section="rule-sets" />

    <TransientFeedback :success="message" :error="error" success-title="规则集操作已完成" error-title="规则集操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(search || rendererFilter || activeFilter)" @clear="resetFilters">
          <WorkbenchFilterInput v-model="search" label="搜索" placeholder="名称、标识、说明或地址" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="rendererFilter" label="输出格式" :options="rendererFilterOptions" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="activeFilter" label="可用状态" :options="activeOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable v-if="ruleSets.length" caption="规则集列表；来源集中维护，模板引用数量直接展示" :row-count="total" :min-width="1040" table-class="subscription-rule-set-table">
        <thead><tr><th class="table-primary-column">规则集</th><th>输出格式</th><th data-column-priority="2">资源格式</th><th>状态</th><th class="numeric-column">模板数</th><th data-column-priority="2">更新时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="item in ruleSets" :key="item.id">
          <td class="table-primary-column"><div class="cell-title"><strong>{{ item.name }}</strong><span><code>{{ item.tag }}</code> · {{ sourceHost(item.url) }}</span></div></td>
          <td><StatusBadge tone="info" icon="audit">{{ rendererLabel(item.renderer) }}</StatusBadge></td>
          <td data-column-priority="2"><StatusBadge tone="neutral" icon="database">{{ formatLabel(item) }}</StatusBadge></td>
          <td><StatusBadge :tone="item.is_active ? 'success' : 'neutral'" :icon="item.is_active ? 'check' : 'minus'">{{ item.is_active ? '可供选择' : '已停用' }}</StatusBadge></td>
          <td class="numeric-column">{{ item.usage_count }}</td>
          <td data-column-priority="2"><TimeBadge :value="item.updated_at" /></td>
          <td class="table-action-column"><RowActions :label="`${item.name} 的操作`" :trigger-key="`subscription-rule-set-${item.id}`">
            <UiButton variant="secondary" size="sm" type="button" :loading="editingID === item.id" @click="openEdit(item)"><UiIcon name="edit" />编辑</UiButton>
            <UiButton variant="danger" size="sm" type="button" :loading="deletingID === item.id" :disabled="item.usage_count > 0" :title="item.usage_count > 0 ? `仍被 ${item.usage_count} 个模板引用` : undefined" @click="remove(item)">删除</UiButton>
          </RowActions></td>
        </tr></tbody>
      </DataTable>
      <EmptyState v-else-if="!loading" icon="audit" :title="search || rendererFilter || activeFilter ? '没有匹配规则集' : '还没有规则集'" :description="search || rendererFilter || activeFilter ? '调整或清除筛选条件后重试。' : '创建可复用规则集后，订阅模板即可通过远程搜索选择；临时来源仍可在模板中快捷添加。'">
        <template v-if="!search && !rendererFilter && !activeFilter" #actions><UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建规则集</UiButton></template>
      </EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <ModalDialog :open="editorOpen" :dirty="editorState.dirty.value" :title="form.id ? '编辑规则集' : '新建规则集'" description="规则集只维护远端来源；匹配后的代理、直连或拦截动作由每个订阅模板单独选择。" size="lg" :busy="saving" :return-focus-selector="form.id ? `[data-row-action-trigger='subscription-rule-set-${form.id}']` : ''" @close="closeEditor">
      <form id="subscription-rule-set-form" ref="formElement" class="rule-set-editor" novalidate @submit.prevent="save">
        <div v-if="form.id" class="editor-meta"><StatusBadge tone="neutral" icon="history">版本 {{ form.revision }}</StatusBadge><StatusBadge tone="info" icon="audit">{{ form.usage_count }} 个模板引用</StatusBadge><TimeBadge :value="form.updated_at" /></div>
        <PageAlert v-if="revisionConflict" tone="warning" title="规则集已在其他会话更新">
          当前草稿基于旧版本，请重新加载最新来源后继续编辑。
          <template #actions><UiButton variant="secondary" size="sm" type="button" :loading="editingID === form.id" @click="reloadEditor"><UiIcon name="refresh" />重新加载最新版本</UiButton></template>
        </PageAlert>
        <PageAlert v-if="editorErrors.formError.value" tone="danger" title="无法保存规则集">{{ editorErrors.formError.value }}</PageAlert>
        <div class="form-grid">
          <FormField label="规则集名称" name="subscription-rule-set-name" :error="editorErrors.fields.name" hint="用于管理员检索和模板选择。" required>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.name" v-bind="controlAttrs" maxlength="80" placeholder="例如：广告拦截" /></template>
          </FormField>
          <FormField label="输出格式" name="subscription-rule-set-renderer" :error="editorErrors.fields.renderer" :hint="form.usage_count ? '被模板引用后不能切换输出格式。' : '决定客户端规则资源结构。'" required>
            <template #default="{ controlAttrs }"><UiSelect v-model="form.renderer" v-bind="controlAttrs" :options="rendererOptions" :disabled="form.usage_count > 0" /></template>
          </FormField>
          <FormField label="规则标识" name="subscription-rule-set-tag" :error="editorErrors.fields.tag" hint="同一输出格式内唯一；会写入客户端配置。" required>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.tag" v-bind="controlAttrs" maxlength="64" placeholder="例如：reject-ads" /></template>
          </FormField>
          <FormField label="资源格式" name="subscription-rule-set-format" :error="editorErrors.fields.format" required>
            <template #default="{ controlAttrs }"><UiSelect v-model="form.format" v-bind="controlAttrs" :options="formatOptions" @change="normalizeFormat" /></template>
          </FormField>
          <FormField v-if="form.renderer === 'clash'" label="规则行为" name="subscription-rule-set-behavior" :error="editorErrors.fields.behavior" required>
            <template #default="{ controlAttrs }"><UiSelect v-model="form.behavior" v-bind="controlAttrs" :options="clashRuleBehaviorOptions" @change="normalizeFormat" /></template>
          </FormField>
          <FormField label="更新间隔" name="subscription-rule-set-interval" :error="editorErrors.fields.interval" hint="60 秒至 7 天。" required>
            <template #default="{ controlAttrs }"><UiNumberInput v-model="form.interval" v-bind="controlAttrs" :min="60" :max="604800" suffix=" 秒" /></template>
          </FormField>
          <FormField label="远端地址" name="subscription-rule-set-url" :error="editorErrors.fields.url" hint="仅支持不含账号密码和片段的 HTTP(S) 地址。" required full>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.url" v-bind="controlAttrs" type="url" maxlength="2048" placeholder="https://example.com/rules/ads.yaml" /></template>
          </FormField>
          <FormField label="用途说明" name="subscription-rule-set-description" :error="editorErrors.fields.description" full>
            <template #default="{ controlAttrs }"><UiTextarea v-model.trim="form.description" v-bind="controlAttrs" rows="3" maxlength="255" placeholder="说明规则来源、覆盖范围或维护责任。" /></template>
          </FormField>
          <FormField label="可用状态" name="subscription-rule-set-active" :error="editorErrors.fields.is_active" full>
            <label class="check-field"><UiCheckbox v-model="form.is_active" /><span><strong>允许模板选择</strong><br /><small class="field-hint">停用后不会出现在新选择结果中；已有模板引用仍继续生效。</small></span></label>
          </FormField>
        </div>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton><UiButton form="subscription-rule-set-form" type="submit" :loading="saving" :disabled="revisionConflict">保存规则集</UiButton></template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import {
  createSubscriptionRuleSet,
  deleteSubscriptionRuleSet,
  fetchSubscriptionRuleSet,
  fetchSubscriptionRuleSetsPage,
  updateSubscriptionRuleSet,
  type SubscriptionRenderer,
  type SubscriptionRuleSet,
} from '../api/client'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import SubscriptionTemplateSectionNav from '../components/SubscriptionTemplateSectionNav.vue'
import TablePager from '../components/TablePager.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import UiIcon from '../components/UiIcon.vue'
import UiInput from '../components/UiInput.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import UiSelect from '../components/UiSelect.vue'
import UiTextarea from '../components/UiTextarea.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { confirmAction } from '../utils/feedback'
import { preserveAdminReturnTo } from '../utils/navigation'
import { clashRuleBehaviorOptions, subscriptionRuleFormatOptions, subscriptionTemplateOutput, subscriptionTemplateOutputOptions } from '../utils/subscriptionTemplateEditor'
import { collectFieldErrors, isHttpUrl, isIntegerInRange, isOneOf, isUtf8LengthInRange } from '../utils/validation'

type SupportedRenderer = Exclude<SubscriptionRenderer, 'unsupported'>
const route = useRoute()
const router = useRouter()
const rendererOptions = subscriptionTemplateOutputOptions.map(item => ({ label: item.label, value: item.value }))
const rendererFilterOptions = [{ label: '全部格式', value: '' }, ...rendererOptions]
const activeOptions = [{ label: '全部状态', value: '' }, { label: '可供选择', value: 'true' }, { label: '已停用', value: 'false' }]
const allowedPageSizes = [25, 50, 100]
const search = ref(String(route.query.q || ''))
const rendererFilter = ref(String(route.query.renderer || ''))
const activeFilter = ref(String(route.query.active || ''))
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const emptyForm = () => ({
  id: 0, name: '', description: '', renderer: 'clash' as SupportedRenderer, tag: '', url: '',
  behavior: 'classical' as 'domain' | 'ipcidr' | 'classical' | undefined,
  format: 'yaml' as SubscriptionRuleSet['format'], interval: 86400, is_active: true,
  revision: 0, usage_count: 0, created_at: '', updated_at: '',
})
const form = reactive(emptyForm())
const saving = ref(false), editorOpen = ref(false), editingID = ref(0), deletingID = ref(0)
const message = ref('')
const revisionConflict = ref(false)
const formElement = ref<HTMLElement | null>(null)
let suppressRendererChange = false
const editorState = useDirtyForm(() => form)
const editorErrors = useFormErrors()
useUnsavedChangesGuard(
  () => editorOpen.value && editorState.dirty.value,
  () => editorState.confirmDiscard({ title: '放弃规则集修改？', message: '离开后，尚未保存的规则来源调整将丢失。', confirmText: '离开页面' }),
)
const fieldMap: Record<string, string> = {
  name: 'name', description: 'description', renderer: 'renderer', tag: 'tag', url: 'url',
  behavior: 'behavior', format: 'format', interval: 'interval', is_active: 'is_active',
}
for (const [source, field] of [
  [() => form.name, 'name'], [() => form.description, 'description'], [() => form.renderer, 'renderer'],
  [() => form.tag, 'tag'], [() => form.url, 'url'], [() => form.behavior, 'behavior'],
  [() => form.format, 'format'], [() => form.interval, 'interval'], [() => form.is_active, 'is_active'],
] as Array<[() => unknown, string]>) watch(source, () => editorErrors.clear(field))
const formatOptions = computed(() => subscriptionRuleFormatOptions(form.renderer).map(option => ({
  ...option,
  disabled: form.renderer === 'clash' && option.value === 'mrs' && form.behavior === 'classical',
})))
const { items: ruleSets, total, loading, refreshing, error, load } = useRemoteTable<SubscriptionRuleSet>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchSubscriptionRuleSetsPage({
    q: search.value || undefined,
    renderer: rendererFilter.value ? rendererFilter.value as SupportedRenderer : undefined,
    active: activeFilter.value === '' ? undefined : activeFilter.value === 'true',
    offset: offset.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '规则集加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = { query: {
    ...preserveAdminReturnTo(route.query.return_to),
    ...(search.value ? { q: search.value } : {}),
    ...(rendererFilter.value ? { renderer: rendererFilter.value } : {}),
    ...(activeFilter.value ? { active: activeFilter.value } : {}),
    ...(page > 1 ? { page: String(page) } : {}),
    ...(limit.value !== 50 ? { limit: String(limit.value) } : {}),
    ...(editorOpen.value ? { rule_set: form.id ? String(form.id) : 'new' } : {}),
  } }
  await (replace ? router.replace(location) : router.push(location))
}
async function applyFilters() { offset.value = 0; await syncURL(); await load() }
async function resetFilters() { search.value = ''; rendererFilter.value = ''; activeFilter.value = ''; await applyFilters() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await load() }
async function openCreate() { Object.assign(form, emptyForm()); revisionConflict.value = false; editorErrors.clear(); editorState.markClean(); editorOpen.value = true; await syncURL() }
async function openEdit(item: SubscriptionRuleSet) { await router.push({ query: { ...route.query, rule_set: String(item.id) } }) }
async function loadEditor(id: number) {
  editingID.value = id
  error.value = ''
  try {
    suppressRendererChange = true
    Object.assign(form, await fetchSubscriptionRuleSet(id))
    suppressRendererChange = false
    revisionConflict.value = false
    editorErrors.clear()
    editorState.markClean()
    editorOpen.value = true
  } catch (cause: any) {
    suppressRendererChange = false
    error.value = cause?.response?.data?.message || '规则集详情加载失败。'
  } finally {
    editingID.value = 0
  }
}
async function reloadEditor() { if (form.id) await loadEditor(form.id) }
async function closeEditor() { if (saving.value) return; editorState.markClean(); editorOpen.value = false; await syncURL() }
function normalizeFormat() {
  if (form.renderer !== 'clash') form.behavior = undefined
  else {
    form.behavior ||= 'classical'
    if (form.format === 'mrs' && form.behavior === 'classical') form.behavior = 'domain'
  }
}
function resetRendererDefaults(renderer: SupportedRenderer) {
  form.format = renderer === 'znet-sink' ? 'domain_list' : renderer === 'sing-box' ? 'source' : 'yaml'
  form.behavior = renderer === 'clash' ? 'classical' : undefined
}
async function save() {
  form.name = form.name.trim(); form.description = form.description.trim(); form.tag = form.tag.trim(); form.url = form.url.trim()
  const valid = await editorErrors.applyValidation(collectFieldErrors({
    name: !isUtf8LengthInRange(form.name, 1, 80, true) && '规则集名称需包含 1 到 80 个字符。',
    description: !isUtf8LengthInRange(form.description, 0, 255) && '规则集说明不能超过 255 个字符。',
    renderer: !isOneOf(form.renderer, ['znet-sink', 'clash', 'sing-box']) && '请选择系统支持的输出格式。',
    tag: !/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(form.tag) && '标识仅允许字母、数字、点、下划线和连字符。',
    url: (!isHttpUrl(form.url) || form.url.length > 2048) && '请输入不含账号密码和片段的 HTTP(S) 地址。',
    interval: !isIntegerInRange(form.interval, 60, 604800) && '更新间隔需在 60 秒到 7 天之间。',
    format: form.renderer === 'clash' && form.format === 'mrs' && form.behavior === 'classical' && 'MRS 格式不支持经典规则。',
  }), formElement, '请更正规则集来源后再保存。')
  if (!valid) return
  saving.value = true; message.value = ''
  try {
    const payload = {
      name: form.name, description: form.description, renderer: form.renderer, tag: form.tag, url: form.url,
      behavior: form.renderer === 'clash' ? form.behavior : undefined, format: form.format, interval: Number(form.interval),
      is_active: form.is_active, ...(form.id ? { expected_revision: form.revision } : {}),
    }
    if (form.id) await updateSubscriptionRuleSet(form.id, payload); else await createSubscriptionRuleSet(payload)
    editorState.markClean(); editorOpen.value = false; revisionConflict.value = false; message.value = '规则集已保存。'
    await syncURL(true); await load()
  } catch (cause: any) {
    if (cause?.response?.status === 409) {
      revisionConflict.value = true
      editorErrors.formError.value = '服务器版本已变化，请重新加载当前规则集。'
    } else {
      await editorErrors.applyApiError(cause, '规则集保存失败，请检查表单内容。', formElement, fieldMap)
    }
  } finally {
    saving.value = false
  }
}
async function remove(item: SubscriptionRuleSet) {
  if (item.usage_count > 0 || !await confirmAction({ title: '删除规则集', message: `删除“${item.name}”后无法恢复。`, confirmText: '确认删除', tone: 'danger' })) return
  deletingID.value = item.id; message.value = ''
  try { await deleteSubscriptionRuleSet(item.id); message.value = '规则集已删除。'; await load() }
  catch (cause: any) { error.value = cause?.response?.data?.message || '规则集删除失败。' }
  finally { deletingID.value = 0 }
}
function rendererLabel(renderer: SubscriptionRenderer) { return subscriptionTemplateOutput(renderer)?.label || renderer }
function formatLabel(item: SubscriptionRuleSet) { return item.renderer === 'clash' && item.behavior ? `${item.format} · ${item.behavior}` : item.format }
function sourceHost(value: string) { try { return new URL(value).host } catch { return value } }

watch(() => form.renderer, (next, previous) => { if (!suppressRendererChange && next !== previous) resetRendererDefaults(next) }, { flush: 'sync' })
watch(() => route.fullPath, async () => {
  const nextSearch = String(route.query.q || ''), nextRenderer = String(route.query.renderer || ''), nextActive = String(route.query.active || '')
  const rawLimit = Number(route.query.limit), nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  if (nextSearch !== search.value || nextRenderer !== rendererFilter.value || nextActive !== activeFilter.value || nextLimit !== limit.value || nextOffset !== offset.value) {
    search.value = nextSearch; rendererFilter.value = nextRenderer; activeFilter.value = nextActive; limit.value = nextLimit; offset.value = nextOffset; await load()
  }
  const editor = String(route.query.rule_set || '')
  if (!editor) editorOpen.value = false
  else if (editor === 'new' && !editorOpen.value) { Object.assign(form, emptyForm()); editorState.markClean(); editorOpen.value = true }
  else if (/^[1-9]\d*$/.test(editor) && (!editorOpen.value || form.id !== Number(editor))) await loadEditor(Number(editor))
})
onBeforeRouteUpdate(async (to, from) => {
  const nextEditor = String(to.query.rule_set || '')
  const currentEditor = String(from.query.rule_set || '')
  if (nextEditor === currentEditor || !editorOpen.value || !editorState.dirty.value) return true
  return editorState.confirmDiscard({
    title: '放弃规则集修改？',
    message: '切换或关闭规则集编辑器后，当前草稿将丢失。',
    confirmText: '放弃修改',
  })
})
onMounted(async () => {
  await load()
  const editor = String(route.query.rule_set || '')
  if (editor === 'new') { Object.assign(form, emptyForm()); editorState.markClean(); editorOpen.value = true }
  else if (/^[1-9]\d*$/.test(editor)) await loadEditor(Number(editor))
})
</script>

<style scoped>
:deep(.subscription-rule-set-table code){color:var(--code-text);background:var(--code-soft);padding:2px 5px;border-radius:5px}
.rule-set-editor{display:grid;gap:14px}
.editor-meta{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.check-field{min-height:var(--control-height);align-items:center}
</style>
