<template>
  <section class="standard-page">
    <PageHeader title="订阅模板" description="管理用户可选的订阅格式、规则集和输出配置；列表只读取摘要，编辑时再加载详细配置。" eyebrow="Delivery">
      <template #actions>
        <PageRefreshButton label="刷新订阅模板" :loading="loading" @click="load" />
        <UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建模板</UiButton>
      </template>
    </PageHeader>

    <SubscriptionTemplateSectionNav section="templates" />

    <TransientFeedback :success="message" :error="error" success-title="模板操作已完成" error-title="模板操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(search || activeFilter)" @clear="resetFilters">
          <WorkbenchFilterInput v-model="search" label="搜索" placeholder="名称、链接标识或说明" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="activeFilter" label="可用状态" :options="activeOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable v-if="templates.length" caption="订阅模板列表；状态使用图标标签，排序直接显示数字，更新时间经过格式化并保留精确时间提示" :row-count="total" :min-width="980" table-class="template-table">
          <thead><tr><th class="table-primary-column">模板</th><th data-column-priority="2">链接参数</th><th data-column-priority="3">输出格式</th><th>状态</th><th class="numeric-column" data-column-priority="3">排序</th><th data-column-priority="2">更新时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
          <tbody><tr v-for="item in templates" :key="item.id">
            <td class="table-primary-column"><div class="cell-title"><strong>{{ item.name }}</strong><span>{{ item.description || '暂无说明' }}</span></div></td>
            <td data-column-priority="2"><code>?template={{ item.slug }}</code></td>
            <td data-column-priority="3"><StatusBadge :tone="item.renderer === 'unsupported' ? 'warning' : 'info'" icon="audit" :title="item.content_type || undefined">{{ rendererLabel(item.renderer) }}</StatusBadge></td>
            <td><StatusBadge :tone="item.is_active ? 'success' : 'neutral'" :icon="item.is_active ? 'check' : 'minus'">{{ item.is_active ? '用户可选' : '已停用' }}</StatusBadge></td>
            <td class="numeric-column" data-column-priority="3">{{ item.sort_order }}</td>
            <td data-column-priority="2"><TimeBadge :value="item.updated_at" /></td>
            <td class="table-action-column"><RowActions :label="`${item.name} 的操作`" :trigger-key="`template-${item.id}`">
              <UiButton variant="secondary" size="sm" type="button" :loading="editingID === item.id" :data-template-editor-trigger="item.id" @click="openEdit(item)"><UiIcon name="edit" />编辑</UiButton>
              <UiButton variant="ghost" size="sm" type="button" :loading="statusChangingID === item.id" :disabled="item.renderer === 'unsupported'" @click="toggleActive(item)"><UiIcon :name="item.is_active ? 'minus' : 'check'" />{{ item.is_active ? '停用' : '启用' }}</UiButton>
              <UiButton variant="danger" size="sm" type="button" :loading="deletingID === item.id" @click="remove(item)">删除</UiButton>
            </RowActions></td>
          </tr></tbody>
      </DataTable>
      <EmptyState v-else-if="!loading" icon="audit" :title="search || activeFilter ? '没有匹配模板' : '还没有订阅格式'" :description="search || activeFilter ? '调整或清除筛选条件后重试。' : '尚未配置可供用户选择的订阅输出格式；请创建并选择一个系统支持的格式。'">
        <template v-if="!search && !activeFilter" #actions><UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建模板</UiButton></template>
      </EmptyState>
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <ModalDialog :open="editorOpen" :dirty="editorState.dirty.value" :title="form.id ? '编辑订阅模板' : '新建订阅模板'" description="选择目标客户端格式；完整配置格式支持策略与规则定制，节点订阅格式只负责安全生成节点。" size="xl" :busy="saving || previewing" :return-focus-selector="form.id ? `[data-row-action-trigger='template-${form.id}']` : ''" @close="closeEditor">
      <form id="subscription-template-form" ref="formElement" class="template-editor" novalidate @submit.prevent="save">
        <div v-if="form.id" class="editor-meta">
          <StatusBadge tone="neutral" icon="history">版本 {{ form.revision }}</StatusBadge>
          <TimeBadge :value="form.updated_at" />
        </div>
        <div v-if="revisionConflict" class="editor-conflict">
          <PageAlert tone="warning" title="模板已在其他会话更新">当前草稿基于旧版本，继续覆盖会丢失其他管理员的修改。请重新加载最新版本后再编辑。</PageAlert>
          <UiButton variant="secondary" size="sm" type="button" :loading="editingID === form.id" @click="reloadEditor"><UiIcon name="refresh" />重新加载最新版本</UiButton>
        </div>
        <PageAlert v-if="editorErrors.formError.value" tone="danger" title="无法保存模板">{{ editorErrors.formError.value }}</PageAlert>
        <section class="editor-section" aria-labelledby="template-basic-title">
          <header class="editor-section-heading">
            <div><strong id="template-basic-title">基本信息</strong><span>这些信息决定模板在用户端的名称和订阅链接。</span></div>
          </header>
          <div class="form-grid">
            <FormField label="模板名称" name="template-name" :error="editorErrors.fields.name" required>
              <template #default="{ controlAttrs }"><UiInput v-model.trim="form.name" v-bind="controlAttrs" maxlength="80" placeholder="例如：Clash" /></template>
            </FormField>
            <FormField label="链接标识" name="template-slug" :error="editorErrors.fields.slug" hint="仅小写字母、数字和单个连字符；会写入订阅 URL。" required>
              <template #default="{ controlAttrs }"><UiInput v-model.trim="form.slug" v-bind="controlAttrs" maxlength="80" pattern="[a-z0-9]+(?:-[a-z0-9]+)*" placeholder="例如：clash" /></template>
            </FormField>
            <FormField label="说明" name="template-description" :error="editorErrors.fields.description" hint="向管理员说明适用客户端和用途。">
              <template #default="{ controlAttrs }"><UiInput v-model.trim="form.description" v-bind="controlAttrs" maxlength="255" placeholder="例如：适用于 Clash Meta 和 Mihomo" /></template>
            </FormField>
            <FormField label="排序" name="template-sort-order" :error="editorErrors.fields.sort_order" hint="数字越小越靠前；不确定时保留 0。">
              <template #default="{ controlAttrs }"><UiNumberInput v-model="form.sort_order" v-bind="controlAttrs" inputmode="numeric" /></template>
            </FormField>
            <label class="check-field field-full"><UiCheckbox v-model="form.is_active" /><span><strong>允许用户选择</strong><br /><small class="field-hint">停用后已有带此参数的链接会返回未找到。</small></span></label>
          </div>
        </section>

        <section class="editor-section" aria-labelledby="template-output-title">
          <header class="editor-section-heading">
            <div><strong id="template-output-title">输出方式</strong><span>系统负责协议字段、动态节点和凭据注入，不需要编写模板代码。</span></div>
          </header>
          <TemplateOutputModePicker v-model="form.renderer" />

          <div v-if="selectedOutput" class="standard-output-summary">
            <span class="standard-output-icon"><UiIcon name="shield" /></span>
            <div>
              <strong>已绑定 {{ selectedOutput.label }} 渲染器</strong>
              <span v-if="selectedOutput.mode === 'full'">协议和凭据由后端安全生成；策略组、规则集和客户端路由可以继续定制，响应类型固定为 {{ selectedOutput.contentType }}。</span>
              <span v-else>协议和凭据由后端安全生成；该格式只输出客户端节点订阅，不隐式生成策略组或规则，响应类型固定为 {{ selectedOutput.contentType }}。</span>
            </div>
            <StatusBadge tone="success" icon="check">{{ selectedOutput.mode === 'full' ? '完整配置' : '节点订阅' }}</StatusBadge>
          </div>
          <PageAlert v-else tone="warning" title="旧模板已停用">该记录来自无法自动转换的旧技术模板。请选择一个系统支持的输出格式后才能重新启用。</PageAlert>
          <SubscriptionTemplateCustomizer v-if="selectedOutput?.mode === 'full'" v-model="form.customization" :renderer="form.renderer" :error="editorErrors.fields['template-customization']" @raw-error="rawCustomizationError = $event" />
          <PageAlert v-else-if="selectedOutput" tone="info" title="该标准订阅协议只承载节点">
            {{ selectedOutput.label }} 模板只负责导出兼容节点与凭据。DNS、TUN、本地代理开关、运行模式、DIRECT、REJECT、策略组和规则集都属于客户端全局设置，无法写入这种节点订阅；界面不会保存后又静默丢弃这些字段。
          </PageAlert>
        </section>

        <section class="template-preview" aria-labelledby="template-preview-title">
          <header><div><strong id="template-preview-title">示例输出</strong><span>使用当前输出格式生成，不会保存草稿。</span></div><UiButton variant="secondary" size="sm" type="button" :loading="previewing" @click="runPreview"><UiIcon name="search" />生成预览</UiButton></header>
          <div v-if="previewResult" class="preview-meta">
            <StatusBadge :tone="previewStale ? 'warning' : 'success'" :icon="previewStale ? 'edit' : 'check'">{{ previewStale ? '模板配置已修改，请重新生成' : '当前模板配置已验证' }}</StatusBadge>
            <span>{{ previewResult.line_count }} 行</span><span>{{ previewResult.bytes.toLocaleString('zh-CN') }} 字节</span>
          </div>
          <PageAlert v-if="previewResult?.truncated" tone="warning" title="预览已截断">渲染结果超过 256 KiB；这里显示可安全传输的前段内容，保存仍按完整 2 MiB 上限校验。</PageAlert>
          <OutputBlock v-if="previewResult" :value="previewResult.content" label="示例渲染结果" :max-length="262144" />
          <div v-else class="preview-empty"><UiIcon name="audit" /><div><strong>尚未生成示例输出</strong><span>生成预览后可以确认客户端最终收到的内容。</span></div></div>
        </section>
      </form>
      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="saving || previewing" @click="requestClose">取消</UiButton>
        <UiButton form="subscription-template-form" type="submit" :loading="saving" :disabled="previewing || revisionConflict">校验并保存</UiButton>
      </template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { createSubscriptionTemplate, deleteSubscriptionTemplate, fetchSubscriptionTemplate, fetchSubscriptionTemplatesPage, previewSubscriptionTemplate, updateSubscriptionTemplate, type SubscriptionRenderer, type SubscriptionTemplate, type SubscriptionTemplateCustomization, type SubscriptionTemplatePreview } from '../api/client'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import EmptyState from '../components/EmptyState.vue'
import FormField from '../components/FormField.vue'
import ModalDialog from '../components/ModalDialog.vue'
import OutputBlock from '../components/OutputBlock.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import SubscriptionTemplateSectionNav from '../components/SubscriptionTemplateSectionNav.vue'
import SubscriptionTemplateCustomizer from '../components/SubscriptionTemplateCustomizer.vue'
import TablePager from '../components/TablePager.vue'
import TemplateOutputModePicker from '../components/TemplateOutputModePicker.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import UiNumberInput from '../components/UiNumberInput.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useRemoteTable } from '../composables/useRemoteTable'
import { confirmAction } from '../utils/feedback'
import { preserveAdminReturnTo } from '../utils/navigation'
import {
  defaultSubscriptionCustomization,
  normalizeSubscriptionCustomization,
  subscriptionPolicyGroupTypeOptions,
  subscriptionRendererSupportsPolicyConfig,
  subscriptionRendererSupportsRuntimeNetwork,
  subscriptionTemplateOutput,
  subscriptionTemplateOutputOptions,
  type SupportedSubscriptionRenderer,
} from '../utils/subscriptionTemplateEditor'
import { collectFieldErrors, isHttpUrl, isIntegerInRange, isOneOf, isSlug, isUtf8LengthInRange } from '../utils/validation'

type EditableRenderer = SubscriptionRenderer | SupportedSubscriptionRenderer

const route = useRoute()
const router = useRouter()
const activeOptions = [{ label: '全部状态', value: '' }, { label: '用户可选', value: 'true' }, { label: '已停用', value: 'false' }]
const supportedRenderers = subscriptionTemplateOutputOptions.map(option => option.value)
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const activeFilter = ref(typeof route.query.active === 'string' ? route.query.active : '')
const emptyForm = () => ({ id: 0, name: '', slug: '', description: '', renderer: 'clash' as EditableRenderer, customization: defaultSubscriptionCustomization('clash'), content_type: 'application/yaml' as SubscriptionTemplate['content_type'], is_active: true, sort_order: 0, revision: 0, created_at: '', updated_at: '' })
const saving = ref(false), previewing = ref(false), editorOpen = ref(false)
const editingID = ref(0), deletingID = ref(0), statusChangingID = ref(0)
const message = ref('')
const editorRouteKey = ref(typeof route.query.template === 'string' ? route.query.template : '')
const revisionConflict = ref(false)
const rawCustomizationError = ref('')
const previewResult = ref<SubscriptionTemplatePreview | null>(null)
const previewSource = ref('')
const formElement = ref<HTMLElement | null>(null)
const form = reactive(emptyForm())
const rendererDrafts = new Map<EditableRenderer, SubscriptionTemplateCustomization>()
let suppressRendererDraftSwitch = false
const editorState = useDirtyForm(() => form)
useUnsavedChangesGuard(
  () => editorOpen.value && editorState.dirty.value,
  () => editorState.confirmDiscard({
    title: '放弃订阅模板修改？',
    message: '离开订阅模板后，尚未保存的名称、状态和输出格式将丢失。',
    confirmText: '离开页面',
  }),
)
const editorErrors = useFormErrors()
const templateFieldMap: Record<string, string> = { name: 'name', slug: 'slug', description: 'description', renderer: 'renderer', customization: 'template-customization', sort_order: 'sort_order' }
const currentPreviewSource = computed(() => JSON.stringify({ renderer: form.renderer, customization: form.customization }))
const previewStale = computed(() => Boolean(previewResult.value) && previewSource.value !== currentPreviewSource.value)
const selectedOutput = computed(() => subscriptionTemplateOutput(form.renderer))
for (const field of Object.keys(templateFieldMap)) {
  if (field === 'customization') continue
  watch(() => form[field as keyof typeof form], () => editorErrors.clear(field))
}
watch(() => JSON.stringify(form.customization), () => editorErrors.clear('template-customization'))
let detailRequestSequence = 0
const { items: templates, total, loading, refreshing, error, load } = useRemoteTable<SubscriptionTemplate>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchSubscriptionTemplatesPage({ q: search.value || undefined, active: activeFilter.value === '' ? undefined : activeFilter.value === 'true', offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '订阅模板加载失败。',
  onOffsetCorrected: () => syncURL(true),
})

async function syncURL(replace = false) {
  const page = Math.floor(offset.value / limit.value) + 1
  const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(search.value ? { q: search.value } : {}), ...(activeFilter.value ? { active: activeFilter.value } : {}), ...(page > 1 ? { page: String(page) } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}), ...(editorRouteKey.value ? { template: editorRouteKey.value } : {}) } }
  await (replace ? router.replace(location) : router.push(location))
}

async function applyFilters() { offset.value = 0; await syncURL(); await load() }
async function resetFilters() { search.value = ''; activeFilter.value = ''; await applyFilters() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await load() }
async function openCreate() { await router.push({ query: { ...route.query, template: 'new' } }) }
async function openEdit(item: SubscriptionTemplate) { await router.push({ query: { ...route.query, template: String(item.id) } }) }

function rendererLabel(renderer: SubscriptionRenderer | string) {
  return subscriptionTemplateOutput(renderer as EditableRenderer)?.label || '旧格式（需转换）'
}

async function loadEditor(id: number) {
  const sequence = ++detailRequestSequence
  editingID.value = id; error.value = ''
  try {
    const detail = await fetchSubscriptionTemplate(id)
    if (sequence !== detailRequestSequence) return
    suppressRendererDraftSwitch = true
    Object.assign(form, detail, { customization: normalizeSubscriptionCustomization(detail.renderer, detail.customization) })
    suppressRendererDraftSwitch = false
    rendererDrafts.clear()
    rendererDrafts.set(form.renderer as EditableRenderer, cloneCustomization(form.customization))
    rawCustomizationError.value = ''
    editorState.markClean(); editorErrors.clear(); revisionConflict.value = false
    previewResult.value = null; previewSource.value = ''; editorOpen.value = true
  } catch (e: any) {
    if (sequence === detailRequestSequence) error.value = e?.response?.data?.message || '订阅模板详情加载失败。'
  } finally {
    if (sequence === detailRequestSequence) editingID.value = 0
  }
}

async function syncEditorFromRoute() {
  const target = typeof route.query.template === 'string' ? route.query.template : ''
  editorRouteKey.value = target
  if (!target) {
    detailRequestSequence++
    editorOpen.value = false; editingID.value = 0; revisionConflict.value = false
    return
  }
  if (target === 'new') {
    if (editorOpen.value && form.id === 0) return
    suppressRendererDraftSwitch = true
    Object.assign(form, emptyForm())
    suppressRendererDraftSwitch = false
    rendererDrafts.clear()
    rendererDrafts.set(form.renderer as EditableRenderer, cloneCustomization(form.customization))
    rawCustomizationError.value = ''
    editorState.markClean(); editorErrors.clear()
    revisionConflict.value = false; previewResult.value = null; previewSource.value = ''; editorOpen.value = true
    return
  }
  const id = Number(target)
  if (!Number.isInteger(id) || id <= 0) {
    editorRouteKey.value = ''
    await syncURL(true)
    return
  }
  if (editorOpen.value && form.id === id) return
  await loadEditor(id)
}

async function closeEditor() {
  if (saving.value || previewing.value) return
  editorState.markClean(); editorOpen.value = false; editorRouteKey.value = ''
  revisionConflict.value = false; await syncURL()
}

async function reloadEditor() {
  if (!form.id) return
  await loadEditor(form.id)
}

async function runPreview() {
  const customizationError = rawCustomizationError.value || validateCustomizationDraft(form.renderer as EditableRenderer, form.customization)
  const valid = await editorErrors.applyValidation(collectFieldErrors({
    renderer: !isOneOf(form.renderer, supportedRenderers) && '请选择系统支持的订阅输出格式。',
    'template-customization': customizationError,
  }), formElement, '请选择有效的输出格式后再生成预览。')
  if (!valid) return
  previewing.value = true
  try {
    previewResult.value = await previewSubscriptionTemplate({ renderer: form.renderer as SubscriptionRenderer, customization: form.customization })
    previewSource.value = currentPreviewSource.value
  } catch (e: any) {
    await editorErrors.applyApiError(e, '示例输出生成失败，请检查所选格式。', formElement, templateFieldMap)
  } finally {
    previewing.value = false
  }
}

async function save() {
  form.name = form.name.trim()
  form.slug = form.slug.trim().toLowerCase()
  form.description = form.description.trim()
  if (subscriptionRendererSupportsPolicyConfig(form.renderer as EditableRenderer)) {
    for (const group of form.customization.policy_groups) {
      group.id = group.id.trim().toLowerCase()
      group.name = group.name.trim()
      group.include_pattern = (group.include_pattern || '').trim()
      group.exclude_pattern = (group.exclude_pattern || '').trim()
      group.probe_url = (group.probe_url || '').trim()
    }
    for (const rule of form.customization.rule_sets) {
      if (!rule.rule_set_id) {
        rule.tag = (rule.tag || '').trim()
        rule.url = (rule.url || '').trim()
      }
    }
  }
  const customizationError = rawCustomizationError.value || validateCustomizationDraft(form.renderer as EditableRenderer, form.customization)
  const valid = await editorErrors.applyValidation(collectFieldErrors({
    name: !isUtf8LengthInRange(form.name, 1, 80, true) && '模板名称需包含 1 到 80 个 UTF-8 字节。',
    slug: !isSlug(form.slug, 80) && '链接标识只能包含小写字母、数字和单个连字符，且不能超过 80 个 UTF-8 字节。',
    description: !isUtf8LengthInRange(form.description, 0, 255) && '模板说明不能超过 255 个 UTF-8 字节。',
    renderer: !isOneOf(form.renderer, supportedRenderers) && '请选择系统支持的订阅输出格式。',
    'template-customization': customizationError,
    sort_order: !isIntegerInRange(form.sort_order, Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER) && '排序必须为整数。',
  }), formElement, '请更正标记字段后再校验模板。')
  if (!valid) return
  saving.value = true; message.value = ''
  try {
    const payload = { name: form.name, slug: form.slug, description: form.description, renderer: form.renderer as SubscriptionRenderer, customization: form.customization, is_active: form.is_active, sort_order: Number(form.sort_order || 0), ...(form.id ? { expected_revision: form.revision } : {}) }
    if (form.id) await updateSubscriptionTemplate(form.id, payload); else await createSubscriptionTemplate(payload)
    editorState.markClean(); editorOpen.value = false; editorRouteKey.value = ''; revisionConflict.value = false
    message.value = '订阅模板格式和输出配置已保存。'; await syncURL(true); await load()
  } catch (e: any) {
    if (e?.response?.status === 409) {
      revisionConflict.value = true
    } else {
      await editorErrors.applyApiError(e, '订阅模板保存失败，请检查表单内容。', formElement, templateFieldMap)
    }
  }
  finally { saving.value = false }
}

async function remove(item: SubscriptionTemplate) {
  if (!await confirmAction({ title: '删除订阅模板', message: `删除“${item.name}”后，使用 ?template=${item.slug} 的订阅链接会立即失效。`, confirmText: '确认删除', tone: 'danger' })) return
  deletingID.value = item.id; error.value = ''; message.value = ''
  try { await deleteSubscriptionTemplate(item.id); message.value = '订阅模板已删除。'; await load() }
  catch (e: any) { error.value = e?.response?.data?.message || '订阅模板删除失败。' }
  finally { deletingID.value = 0 }
}

async function toggleActive(item: SubscriptionTemplate) {
  if (item.is_active && !await confirmAction({ title: '停用订阅模板', message: `停用“${item.name}”后，用户将不能再选择或使用 ?template=${item.slug}。`, confirmText: '确认停用', tone: 'danger' })) return
  statusChangingID.value = item.id
  error.value = ''
  message.value = ''
  try {
    const detail = await fetchSubscriptionTemplate(item.id)
    await updateSubscriptionTemplate(item.id, {
      name: detail.name,
      slug: detail.slug,
      description: detail.description,
      renderer: detail.renderer,
      customization: detail.customization,
      is_active: !detail.is_active,
      sort_order: detail.sort_order,
      expected_revision: detail.revision,
    })
    message.value = detail.is_active ? '订阅模板已停用。' : '订阅模板已启用。'
    await load()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || '订阅模板状态更新失败。'
  } finally {
    statusChangingID.value = 0
  }
}

watch(() => route.fullPath, async () => {
  const nextSearch = typeof route.query.q === 'string' ? route.query.q : ''
  const nextActive = typeof route.query.active === 'string' ? route.query.active : ''
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50
  const nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit
  const listChanged = nextSearch !== search.value || nextActive !== activeFilter.value || nextLimit !== limit.value || nextOffset !== offset.value
  if (listChanged) {
    search.value = nextSearch; activeFilter.value = nextActive; limit.value = nextLimit; offset.value = nextOffset; await load()
  }
  await syncEditorFromRoute()
})

watch(() => form.renderer, (next, previous) => {
  if (suppressRendererDraftSwitch || next === previous || !supportedRenderers.includes(next as any)) return
  if (supportedRenderers.includes(previous as any)) rendererDrafts.set(previous as EditableRenderer, cloneCustomization(form.customization))
  const nextDraft = rendererDrafts.get(next as EditableRenderer)
  form.customization = nextDraft ? cloneCustomization(nextDraft) : defaultSubscriptionCustomization(next as EditableRenderer)
  rawCustomizationError.value = ''
  previewResult.value = null
  previewSource.value = ''
}, { flush: 'sync' })

onBeforeRouteUpdate(async (to, from) => {
  const nextEditor = typeof to.query.template === 'string' ? to.query.template : ''
  const currentEditor = typeof from.query.template === 'string' ? from.query.template : ''
  if (nextEditor === currentEditor || !editorOpen.value || !editorState.dirty.value) return true
  return confirmAction({
    title: '放弃未保存的模板修改？',
    message: '切换或关闭模板编辑器后，当前草稿将丢失。',
    confirmText: '放弃修改',
    tone: 'danger',
  })
})

onMounted(async () => { await load(); await syncEditorFromRoute() })

function cloneCustomization(value: SubscriptionTemplateCustomization): SubscriptionTemplateCustomization {
  return JSON.parse(JSON.stringify(value))
}

function validateCustomizationDraft(renderer: EditableRenderer, customization: SubscriptionTemplateCustomization) {
  if (!subscriptionRendererSupportsPolicyConfig(renderer)) return ''
  if (customization.version !== 3) return '订阅模板配置版本无效。'
  if (!['rule', 'global', 'direct'].includes(customization.mode)) return '运行模式必须是规则、全局代理或全部直连。'
  if (customization.mixed_enabled && !isIntegerInRange(Number(customization.mixed_port), 1, 65535)) return '本地混合入站端口必须在 1 到 65535 之间。'
  if (customization.system_proxy && renderer !== 'sing-box') return '只有 sing-box 支持自动设置系统 HTTP 代理。'
  if (customization.system_proxy && !customization.mixed_enabled) return '自动设置系统 HTTP 代理需要先启用本地混合代理。'
  if (!customization.mixed_enabled && !customization.tun.enabled) return '本地混合代理与 TUN 不能同时关闭。'
  const runtimeError = validateRuntimeCustomization(renderer, customization)
  if (runtimeError) return runtimeError
  if (!customization.policy_groups.length || customization.policy_groups.length > 16) return '策略组数量需在 1 到 16 个之间。'
  const supportedTypes = new Set(subscriptionPolicyGroupTypeOptions(renderer).map(option => option.value))
  const groupIDs = new Set<string>()
  const groupNames = new Set<string>()
  for (const [index, group] of customization.policy_groups.entries()) {
    if (!/^[a-z][a-z0-9-]{0,31}$/.test(group.id)) return `第 ${index + 1} 个策略组标识只能使用小写字母、数字和连字符。`
    if (groupIDs.has(group.id)) return `第 ${index + 1} 个策略组标识重复。`
    groupIDs.add(group.id)
    const name = group.name.trim()
    if (!name || Array.from(name).length > 64 || /[\r\n\t]/.test(name)) return `第 ${index + 1} 个策略组名称需包含 1 到 64 个字符且不能包含控制字符。`
    if (['direct', 'reject', 'block'].includes(name.toLowerCase())) return `第 ${index + 1} 个策略组名称使用了系统保留名称。`
    if (renderer === 'clash' && name.includes(',')) return `第 ${index + 1} 个 Clash 策略组名称不能包含逗号。`
    if (groupNames.has(name.toLowerCase())) return `第 ${index + 1} 个策略组名称重复。`
    groupNames.add(name.toLowerCase())
    if (!supportedTypes.has(group.type)) return `第 ${index + 1} 个策略组类型不受当前输出格式支持。`
    if (new TextEncoder().encode(group.include_pattern || '').length > 256 || new TextEncoder().encode(group.exclude_pattern || '').length > 256) return `第 ${index + 1} 个策略组节点正则不能超过 256 字节。`
    if (group.type === 'urltest' || group.type === 'fallback') {
      if (!isHttpUrl(group.probe_url || '')) return `第 ${index + 1} 个策略组检测地址无效。`
      if ((renderer === 'zero' || renderer === 'znet-sink') && !String(group.probe_url).startsWith('http://')) return `第 ${index + 1} 个 Zero 策略组检测地址必须使用 HTTP。`
      if (!isIntegerInRange(Number(group.interval), 60, 86400)) return `第 ${index + 1} 个策略组检测间隔需在 60 秒到 24 小时之间。`
    }
    if (group.type === 'urltest' && !isIntegerInRange(Number(group.tolerance || 0), 0, 10000)) return `第 ${index + 1} 个策略组延迟容差需在 0 到 10000 毫秒之间。`
  }
  if (!groupIDs.has(customization.main_group)) return '请选择有效的主策略组。'
  for (const [index, group] of customization.policy_groups.entries()) {
    const included = new Set(group.include_groups || [])
    if (included.has(group.id)) return `第 ${index + 1} 个策略组不能包含自身。`
    if ([...included].some(id => !groupIDs.has(id))) return `第 ${index + 1} 个策略组包含了不存在的策略组。`
    if (group.default_group && (!included.has(group.default_group) || group.type !== 'select')) return `第 ${index + 1} 个策略组默认成员配置无效。`
  }
  if (hasPolicyGroupCycle(customization)) return '策略组之间不能形成循环引用。'
  if (!isValidSubscriptionTarget(customization.final, groupIDs, false)) return '最终路由必须指向现有策略组或直连。'
  if (customization.rule_sets.length > 64) return '规则集最多允许 64 个。'
  const tags = new Set<string>()
  const references = new Set<number>()
  for (const [index, rule] of customization.rule_sets.entries()) {
    if (!isValidSubscriptionTarget(rule.target, groupIDs, true)) return `第 ${index + 1} 个规则集出站目标无效。`
    if (rule.rule_set_id) {
      if (!Number.isInteger(rule.rule_set_id) || rule.rule_set_id <= 0) return `第 ${index + 1} 个规则集引用无效。`
      if (references.has(rule.rule_set_id)) return `第 ${index + 1} 个规则集引用重复。`
      references.add(rule.rule_set_id)
      continue
    }
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test((rule.tag || '').trim())) return `第 ${index + 1} 个快捷远端标识格式不正确。`
    const tag = (rule.tag || '').trim().toLowerCase()
    if (tags.has(tag)) return `第 ${index + 1} 个规则集标识重复。`
    tags.add(tag)
    if (!isHttpUrl(rule.url || '') || (rule.url || '').length > 2048) return `第 ${index + 1} 个规则集下载地址无效。`
    if (!isIntegerInRange(Number(rule.interval), 60, 604800)) return `第 ${index + 1} 个规则集更新间隔需在 60 秒到 7 天之间。`
    if (renderer === 'clash' && rule.format === 'mrs' && rule.behavior === 'classical') return `第 ${index + 1} 个规则集的 MRS 格式不支持经典规则。`
  }
  if (new TextEncoder().encode(customization.advanced_source || '').length > 131072) return '高级配置不能超过 128 KiB。'
  return ''
}

function validateRuntimeCustomization(renderer: EditableRenderer, customization: SubscriptionTemplateCustomization) {
  const supportsRuntimeNetwork = subscriptionRendererSupportsRuntimeNetwork(renderer)
  if ((customization.dns.enabled || customization.tun.enabled) && !supportsRuntimeNetwork) return '当前输出格式不支持可视化 DNS 或 TUN 配置。'
  if (customization.tun.enabled) {
    const addresses = customization.tun.addresses.map(value => value.trim()).filter(Boolean)
    if (!addresses.length || addresses.length > 2 || addresses.some(value => !isCIDR(value))) return 'TUN 必须配置一到两个有效 CIDR 地址。'
    if (!isIntegerInRange(Number(customization.tun.mtu), 576, 9000)) return 'TUN MTU 必须在 576 到 9000 之间。'
    if (customization.tun.dns_hijack && !customization.dns.enabled) return 'TUN DNS 劫持需要先启用 DNS。'
  }
  if (!customization.dns.enabled) return ''
  if (!customization.dns.servers.length || customization.dns.servers.length > 8) return 'DNS 服务器数量必须在 1 到 8 个之间。'
  const tags = new Set<string>()
  for (const [index, server] of customization.dns.servers.entries()) {
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(server.tag)) return `第 ${index + 1} 个 DNS 服务器标识无效。`
    if (tags.has(server.tag.toLowerCase())) return `第 ${index + 1} 个 DNS 服务器标识重复。`
    tags.add(server.tag.toLowerCase())
    if (!['system', 'udp', 'tcp', 'doh', 'dot', 'doq'].includes(server.type)) return `第 ${index + 1} 个 DNS 服务器类型无效。`
    if ((renderer === 'zero' || renderer === 'znet-sink') && server.type === 'tcp') return 'Zero 当前不支持 TCP DNS，请选择 UDP、DoH、DoT 或 DoQ。'
    if (server.type !== 'system' && (!isIPAddress(server.host || '') || !isIntegerInRange(Number(server.port), 1, 65535))) return `第 ${index + 1} 个 DNS 服务器地址或端口无效。`
    if (server.type === 'doh' && !(server.path || '').startsWith('/')) return `第 ${index + 1} 个 DoH 路径必须以 / 开头。`
  }
  if (!tags.has(customization.dns.default_server.toLowerCase())) return '默认 DNS 服务器不存在。'
  if (customization.dns.cache_enabled && !isIntegerInRange(Number(customization.dns.cache_capacity), 1, 65536)) return 'DNS 缓存容量必须在 1 到 65536 之间。'
  if (customization.dns.fake_ip_enabled && (!isCIDR(customization.dns.fake_ipv4_range) || (customization.dns.fake_ipv6_range && !isCIDR(customization.dns.fake_ipv6_range)))) return 'Fake-IP 地址池必须使用有效 CIDR。'
  return ''
}

function isIPAddress(value: string) {
  if (/^(?:\d{1,3}\.){3}\d{1,3}$/.test(value)) return value.split('.').every(part => Number(part) >= 0 && Number(part) <= 255)
  return value.includes(':') && /^[0-9a-f:]+$/i.test(value)
}

function isCIDR(value: string) {
  const [address, prefix, ...rest] = value.trim().split('/')
  if (rest.length || prefix === undefined || !/^\d+$/.test(prefix)) return false
  const size = isIPAddress(address) && address.includes(':') ? 128 : isIPAddress(address) ? 32 : -1
  return size > 0 && Number(prefix) >= 0 && Number(prefix) <= size
}

function isValidSubscriptionTarget(target: string, groupIDs: Set<string>, allowReject: boolean) {
  if (target === 'direct') return true
  if (allowReject && target === 'reject') return true
  return target.startsWith('group:') && groupIDs.has(target.slice(6))
}

function hasPolicyGroupCycle(customization: SubscriptionTemplateCustomization) {
  const graph = new Map(customization.policy_groups.map(group => [group.id, group.include_groups || []]))
  const state = new Map<string, number>()
  const visit = (id: string): boolean => {
    if (state.get(id) === 1) return true
    if (state.get(id) === 2) return false
    state.set(id, 1)
    for (const child of graph.get(id) || []) {
      if (visit(child)) return true
    }
    state.set(id, 2)
    return false
  }
  return [...graph.keys()].some(visit)
}
</script>

<style scoped>
:deep(.template-table code){color:var(--code-text);background:var(--code-soft);padding:3px 6px;border-radius:5px}
.template-editor{display:grid;gap:18px}
.editor-meta{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.editor-conflict{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:10px}
.editor-conflict .page-alert{margin:0}
.editor-section{display:grid;gap:12px}
.editor-section-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;padding-bottom:9px;border-bottom:1px solid var(--line)}
.editor-section-heading>div{display:grid;gap:3px}
.editor-section-heading strong{color:var(--text-strong);font-size:12px}
.editor-section-heading span{color:var(--muted);font-size:10px;line-height:1.5}
.standard-output-summary{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:11px;padding:11px 12px;border:1px solid color-mix(in srgb,var(--success) 22%,var(--line));border-radius:9px;background:var(--success-soft)}
.standard-output-icon{display:grid;width:30px;height:30px;place-items:center;border-radius:8px;color:var(--success);background:var(--surface)}
.standard-output-summary>div{display:grid;gap:2px}.standard-output-summary strong{color:var(--text-strong);font-size:11px}.standard-output-summary span{color:var(--muted);font-size:9px;line-height:1.5}
.template-preview{min-width:0;border:1px solid var(--line);border-radius:10px;background:var(--surface)}
.template-preview>header{min-height:52px;display:flex;align-items:center;justify-content:space-between;gap:10px;padding:9px 10px 9px 13px;border-bottom:1px solid var(--line)}
.template-preview>header>div{display:grid;gap:2px}.template-preview>header strong{color:var(--text-strong);font-size:11px}.template-preview>header span{color:var(--muted);font-size:9px}
.preview-meta{display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:9px 12px;border-bottom:1px solid var(--line);color:var(--muted);font-size:9px}
.template-preview>.page-alert{margin:10px}
.template-preview :deep(.output-block){margin:0;border:0;border-radius:0}
.template-preview :deep(.output-block pre){max-height:330px}
.preview-empty{min-height:112px;display:flex;align-items:center;justify-content:center;gap:10px;padding:20px;color:var(--muted);text-align:left}
.preview-empty>div{display:grid;gap:3px}.preview-empty .ui-icon{width:24px;height:24px;color:var(--primary)}.preview-empty strong{color:var(--text-strong);font-size:11px}.preview-empty span{font-size:9px;line-height:1.55}
@media(max-width:900px){.editor-conflict{grid-template-columns:1fr;align-items:start}}
@media(max-width:700px){.standard-output-summary{grid-template-columns:auto minmax(0,1fr)}.standard-output-summary>.status-badge{grid-column:2}}
</style>
