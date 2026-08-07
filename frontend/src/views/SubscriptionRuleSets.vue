<template>
  <section class="standard-page">
    <PageHeader
      title="规则集"
      description="由 Zboard 保存和发布规则正文。远端地址只用于导入，订阅模板会写入平台自己的规则端点。"
      eyebrow="订阅模板"
    >
      <template #actions>
        <PageRefreshButton label="刷新规则集" :loading="loading" @click="load" />
        <UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建规则集</UiButton>
      </template>
    </PageHeader>

    <SubscriptionTemplateSectionNav section="rule-sets" />

    <PageAlert tone="info" title="统一规则源">
      规则正文使用 Zero Rule IR v1 保存。模板只关联规则集和命中动作，Zboard 会为 znet-sink、Clash 和 sing-box 下发各自可直接下载的地址。
    </PageAlert>

    <TransientFeedback :success="message" :error="error" success-title="规则集操作已完成" error-title="规则集操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(search || activeFilter)" @clear="resetFilters">
          <WorkbenchFilterInput v-model="search" label="搜索" placeholder="名称、标识、说明或来源地址" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="activeFilter" label="可用状态" :options="activeOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable
        v-if="ruleSets.length"
        caption="Zboard 自有规则集列表"
        :row-count="total"
        :min-width="1080"
        table-class="subscription-rule-set-table"
      >
        <thead>
          <tr>
            <th class="table-primary-column">规则集</th>
            <th>来源</th>
            <th class="numeric-column">规则数</th>
            <th data-column-priority="2">正文大小</th>
            <th>状态</th>
            <th class="numeric-column">模板数</th>
            <th data-column-priority="2">更新时间</th>
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in ruleSets" :key="item.id">
            <td class="table-primary-column">
              <div class="cell-title">
                <strong>{{ item.name }}</strong>
                <span><code>{{ item.tag }}</code> · {{ item.managed ? 'Zero Rule IR v1' : '旧版外部引用' }}</span>
              </div>
            </td>
            <td>
              <div class="cell-title compact-cell">
                <strong>{{ item.source_url ? sourceHost(item.source_url) : '本地维护' }}</strong>
                <span>{{ sourceFormatLabel(item.source_format) }}</span>
              </div>
            </td>
            <td class="numeric-column">{{ item.managed ? formatNumber(item.rule_count) : '—' }}</td>
            <td data-column-priority="2">{{ item.managed ? formatBytes(item.content_bytes) : '—' }}</td>
            <td>
              <StatusBadge :tone="item.is_active ? 'success' : 'neutral'" :icon="item.is_active ? 'check' : 'minus'">
                {{ item.is_active ? '可供选择' : '已停用' }}
              </StatusBadge>
            </td>
            <td class="numeric-column">{{ item.usage_count }}</td>
            <td data-column-priority="2"><TimeBadge :value="item.updated_at" /></td>
            <td class="table-action-column">
              <RowActions :label="`${item.name} 的操作`" :trigger-key="`subscription-rule-set-${item.id}`">
                <UiButton v-if="item.managed" variant="secondary" size="sm" type="button" :loading="editingID === item.id" @click="openEdit(item)">
                  <UiIcon name="edit" />编辑正文
                </UiButton>
                <UiButton v-if="item.managed && item.source_url" variant="secondary" size="sm" type="button" :loading="importingID === item.id" @click="reimport(item)">
                  <UiIcon name="refresh" />重新导入
                </UiButton>
                <UiButton v-if="item.managed && item.public_url" variant="secondary" size="sm" type="button" @click="copyEndpoint(item)">
                  复制地址
                </UiButton>
                <UiButton
                  variant="danger"
                  size="sm"
                  type="button"
                  :loading="deletingID === item.id"
                  :disabled="item.usage_count > 0"
                  :title="item.usage_count > 0 ? `仍被 ${item.usage_count} 个模板引用` : undefined"
                  @click="remove(item)"
                >删除</UiButton>
              </RowActions>
            </td>
          </tr>
        </tbody>
      </DataTable>

      <EmptyState
        v-else-if="!loading"
        icon="audit"
        :title="search || activeFilter ? '没有匹配规则集' : '还没有规则集'"
        :description="search || activeFilter ? '调整或清除筛选条件后重试。' : '创建本地规则正文或从远端导入，之后即可在订阅模板中关联。'"
      >
        <template v-if="!search && !activeFilter" #actions>
          <UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建规则集</UiButton>
        </template>
      </EmptyState>

      <template #footer>
        <TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" />
      </template>
    </DataWorkbench>

    <ModalDialog
      :open="editorOpen"
      :dirty="editorState.dirty.value"
      :title="form.id ? '编辑规则集' : '新建规则集'"
      description="规则正文由 Zboard 统一维护；模板中的代理、直连或拦截动作不写入规则正文。"
      size="xl"
      :busy="saving"
      :return-focus-selector="form.id ? `[data-row-action-trigger='subscription-rule-set-${form.id}']` : ''"
      @close="closeEditor"
    >
      <form id="managed-rule-set-form" class="rule-set-editor" novalidate @submit.prevent="save">
        <div v-if="form.id" class="editor-meta">
          <StatusBadge tone="neutral" icon="history">版本 {{ form.revision }}</StatusBadge>
          <StatusBadge tone="info" icon="audit">{{ formatNumber(form.rule_count) }} 条规则</StatusBadge>
          <StatusBadge tone="neutral" icon="database">{{ formatBytes(form.content_bytes) }}</StatusBadge>
        </div>

        <PageAlert v-if="revisionConflict" tone="warning" title="规则集已被其他管理员更新">
          当前编辑内容基于旧版本，请重新加载后再继续修改。
          <template #actions>
            <UiButton variant="secondary" size="sm" type="button" :loading="editingID === form.id" @click="reloadEditor">重新加载</UiButton>
          </template>
        </PageAlert>
        <PageAlert v-if="formError" tone="danger" title="无法保存规则集">{{ formError }}</PageAlert>

        <div class="form-grid">
          <FormField label="规则集名称" name="managed-rule-name" :error="fieldErrors.name" hint="用于后台检索和模板选择。" required>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.name" v-bind="controlAttrs" maxlength="80" placeholder="例如：广告拦截" /></template>
          </FormField>

          <FormField label="规则标识" name="managed-rule-tag" :error="fieldErrors.tag" :hint="form.id ? '用于公开地址，创建后不能修改。' : '仅允许字母、数字、点、下划线和连字符。'" required>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.tag" v-bind="controlAttrs" maxlength="64" :disabled="Boolean(form.id)" placeholder="例如：reject-ads" /></template>
          </FormField>

          <FormField label="远端来源格式" name="managed-rule-source-format" :error="fieldErrors.source_format" hint="只影响远端导入；保存后的正文始终为 Zero Rule IR v1。">
            <template #default="{ controlAttrs }"><UiSelect v-model="form.source_format" v-bind="controlAttrs" :options="sourceFormatOptions" /></template>
          </FormField>

          <FormField label="客户端下载间隔" name="managed-rule-interval" :error="fieldErrors.sync_interval" hint="写入订阅配置，60 秒至 7 天。" required>
            <template #default="{ controlAttrs }"><UiNumberInput v-model="form.sync_interval" v-bind="controlAttrs" :min="60" :max="604800" suffix=" 秒" /></template>
          </FormField>

          <FormField label="远端来源地址" name="managed-rule-source-url" :error="fieldErrors.source_url" hint="可选。仅用于首次或手动重新导入，不会直接下发给客户端。" full>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.source_url" v-bind="controlAttrs" type="url" maxlength="2048" placeholder="https://example.com/rules/ads.txt" /></template>
          </FormField>

          <FormField label="用途说明" name="managed-rule-description" :error="fieldErrors.description" full>
            <template #default="{ controlAttrs }"><UiTextarea v-model.trim="form.description" v-bind="controlAttrs" rows="2" maxlength="255" placeholder="说明规则覆盖范围和维护用途。" /></template>
          </FormField>

          <FormField label="Zero Rule IR v1 正文" name="managed-rule-content" :error="fieldErrors.content" hint="新建时留空并填写远端地址，将由服务端导入并转换；填写正文时以正文为准。" required full>
            <template #default="{ controlAttrs }">
              <div class="rule-source-editor">
                <div class="rule-source-toolbar">
                  <span>{{ form.content ? formatBytes(contentByteLength(form.content)) : '尚未填写正文' }}</span>
                  <UiButton variant="secondary" size="sm" type="button" @click="fillEmptyIR">填入空白 IR</UiButton>
                  <UiButton v-if="form.id && form.source_url" variant="secondary" size="sm" type="button" :loading="importingID === form.id" @click="reimportCurrent">从远端覆盖</UiButton>
                </div>
                <UiTextarea v-model="form.content" v-bind="controlAttrs" rows="18" spellcheck="false" placeholder="{&#10;  &quot;version&quot;: 1,&#10;  &quot;rules&quot;: []&#10;}" />
              </div>
            </template>
          </FormField>

          <FormField label="可用状态" name="managed-rule-active" :error="fieldErrors.is_active" full>
            <label class="check-field">
              <UiCheckbox v-model="form.is_active" />
              <span><strong>允许模板选择</strong><br /><small class="field-hint">停用后不会出现在新选择结果中；已有模板引用仍继续生效。</small></span>
            </label>
          </FormField>
        </div>
      </form>

      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton>
        <UiButton form="managed-rule-set-form" type="submit" :loading="saving" :disabled="revisionConflict">保存规则集</UiButton>
      </template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import {
  API_BASE,
  createSubscriptionRuleSet,
  deleteSubscriptionRuleSet,
  fetchSubscriptionRuleSet,
  fetchSubscriptionRuleSetsPage,
  getAuthToken,
  updateSubscriptionRuleSet,
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
import { useDirtyForm, useUnsavedChangesGuard } from '../composables/useFormState'
import { confirmAction } from '../utils/feedback'

interface ManagedRuleSet extends Omit<SubscriptionRuleSet, 'renderer'> {
  renderer: string
  managed: boolean
  source_url?: string
  source_format?: string
  rule_count: number
  content_bytes: number
  content_sha256?: string
  public_url?: string
}

interface ManagedRuleContent {
  id: number
  tag: string
  content: string
  rule_count: number
  content_bytes: number
  content_sha256: string
  revision: number
}

interface RuleSetForm {
  id: number
  name: string
  description: string
  tag: string
  source_url: string
  source_format: string
  sync_interval: number
  is_active: boolean
  content: string
  revision: number
  usage_count: number
  rule_count: number
  content_bytes: number
}

class ManagedRequestError extends Error {
  status: number
  fields: Record<string, string>

  constructor(message: string, status: number, fields: Record<string, string> = {}) {
    super(message)
    this.status = status
    this.fields = fields
  }
}

const activeOptions = [
  { label: '全部状态', value: '' },
  { label: '可供选择', value: 'true' },
  { label: '已停用', value: 'false' },
]
const sourceFormatOptions = [
  { label: 'Zero Rule IR v1', value: 'zero_rule_ir' },
  { label: '域名列表', value: 'domain_list' },
  { label: 'CIDR 列表', value: 'cidr_list' },
  { label: 'Clash classical', value: 'clash_classical' },
]
const allowedPageSizes = [25, 50, 100]
const search = ref('')
const activeFilter = ref('')
const limit = ref(50)
const offset = ref(0)
const total = ref(0)
const ruleSets = ref<ManagedRuleSet[]>([])
const loading = ref(false)
const refreshing = ref(false)
const error = ref('')
const message = ref('')
const editorOpen = ref(false)
const saving = ref(false)
const editingID = ref(0)
const importingID = ref(0)
const deletingID = ref(0)
const revisionConflict = ref(false)
const formError = ref('')
const fieldErrors = ref<Record<string, string>>({})

const emptyForm = (): RuleSetForm => ({
  id: 0,
  name: '',
  description: '',
  tag: '',
  source_url: '',
  source_format: 'zero_rule_ir',
  sync_interval: 86400,
  is_active: true,
  content: '',
  revision: 0,
  usage_count: 0,
  rule_count: 0,
  content_bytes: 0,
})
const form = reactive<RuleSetForm>(emptyForm())
const editorState = useDirtyForm(() => form)
useUnsavedChangesGuard(
  () => editorOpen.value && editorState.dirty.value,
  () => editorState.confirmDiscard({ title: '放弃规则集修改？', message: '尚未保存的规则正文和来源调整将丢失。', confirmText: '放弃修改' }),
)

for (const [source, field] of [
  [() => form.name, 'name'],
  [() => form.description, 'description'],
  [() => form.tag, 'tag'],
  [() => form.source_url, 'source_url'],
  [() => form.source_format, 'source_format'],
  [() => form.sync_interval, 'sync_interval'],
  [() => form.content, 'content'],
] as Array<[() => unknown, string]>) {
  watch(source, () => {
    delete fieldErrors.value[field]
    formError.value = ''
  })
}

async function managedRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body !== undefined) headers.set('Content-Type', 'application/json')
  const token = getAuthToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(`${API_BASE.replace(/\/$/, '')}${path}`, { ...init, headers })
  const payload = await response.json().catch(() => null)
  if (!response.ok) {
    throw new ManagedRequestError(
      payload?.message || `请求失败（HTTP ${response.status}）`,
      response.status,
      payload?.data?.fields || payload?.fields || {},
    )
  }
  return payload?.data as T
}

async function load() {
  const wasLoaded = ruleSets.value.length > 0
  loading.value = !wasLoaded
  refreshing.value = wasLoaded
  error.value = ''
  try {
    const page = await fetchSubscriptionRuleSetsPage({
      q: search.value || undefined,
      active: activeFilter.value === '' ? undefined : activeFilter.value === 'true',
      offset: offset.value,
      limit: limit.value,
    })
    ruleSets.value = page.items as unknown as ManagedRuleSet[]
    total.value = page.page.total
    if (offset.value > 0 && !ruleSets.value.length && total.value > 0) {
      offset.value = Math.max(0, Math.floor((total.value - 1) / limit.value) * limit.value)
      await load()
    }
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || cause?.message || '规则集加载失败。'
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function applyFilters() {
  offset.value = 0
  await load()
}

async function resetFilters() {
  search.value = ''
  activeFilter.value = ''
  await applyFilters()
}

async function changePage(value: { offset: number; limit: number }) {
  offset.value = value.offset
  limit.value = allowedPageSizes.includes(value.limit) ? value.limit : 50
  await load()
}

function openCreate() {
  Object.assign(form, emptyForm())
  fieldErrors.value = {}
  formError.value = ''
  revisionConflict.value = false
  editorOpen.value = true
  editorState.markClean()
}

async function openEdit(item: ManagedRuleSet) {
  editingID.value = item.id
  error.value = ''
  try {
    const [metadata, content] = await Promise.all([
      fetchSubscriptionRuleSet(item.id) as unknown as Promise<ManagedRuleSet>,
      managedRequest<ManagedRuleContent>(`/admin/subscription-rule-sets/${item.id}/content`),
    ])
    Object.assign(form, {
      id: metadata.id,
      name: metadata.name,
      description: metadata.description || '',
      tag: metadata.tag,
      source_url: metadata.source_url || '',
      source_format: metadata.source_format || 'zero_rule_ir',
      sync_interval: metadata.interval || 86400,
      is_active: metadata.is_active,
      content: content.content,
      revision: content.revision,
      usage_count: metadata.usage_count,
      rule_count: content.rule_count,
      content_bytes: content.content_bytes,
    })
    fieldErrors.value = {}
    formError.value = ''
    revisionConflict.value = false
    editorOpen.value = true
    editorState.markClean()
  } catch (cause: any) {
    error.value = cause?.message || cause?.response?.data?.message || '规则集内容加载失败。'
  } finally {
    editingID.value = 0
  }
}

async function reloadEditor() {
  const item = ruleSets.value.find(candidate => candidate.id === form.id)
  if (item) await openEdit(item)
}

function closeEditor() {
  editorOpen.value = false
  revisionConflict.value = false
  fieldErrors.value = {}
  formError.value = ''
}

function fillEmptyIR() {
  if (form.content.trim() && !window.confirm('当前正文不为空，确认替换为空白 Zero Rule IR？')) return
  form.content = JSON.stringify({ version: 1, rules: [] }, null, 2) + '\n'
}

function validateForm() {
  const fields: Record<string, string> = {}
  if (!form.name.trim()) fields.name = '请输入规则集名称。'
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(form.tag.trim())) fields.tag = '规则标识格式不正确。'
  if (!Number.isInteger(form.sync_interval) || form.sync_interval < 60 || form.sync_interval > 604800) fields.sync_interval = '更新间隔必须在 60 秒到 7 天之间。'
  if (form.source_url && !/^https?:\/\//i.test(form.source_url)) fields.source_url = '请输入完整的 HTTP 或 HTTPS 地址。'
  if (!form.content.trim() && !form.source_url.trim()) fields.content = '请输入 Zero Rule IR 正文，或填写远端来源地址。'
  if (form.content.trim()) {
    try {
      JSON.parse(form.content)
    } catch {
      fields.content = '正文不是有效 JSON。'
    }
  }
  fieldErrors.value = fields
  return Object.keys(fields).length === 0
}

async function save() {
  if (!validateForm()) return
  saving.value = true
  formError.value = ''
  message.value = ''
  revisionConflict.value = false
  const payload = {
    name: form.name.trim(),
    description: form.description.trim(),
    tag: form.tag.trim(),
    source_url: form.source_url.trim(),
    source_format: form.source_format,
    sync_interval: form.sync_interval,
    is_active: form.is_active,
    content: form.content.trim() ? form.content : undefined,
    expected_revision: form.id ? form.revision : undefined,
  }
  try {
    if (form.id) await updateSubscriptionRuleSet(form.id, payload as any)
    else await createSubscriptionRuleSet(payload as any)
    message.value = form.id ? '规则集及正文已更新。' : '规则集已创建。'
    editorOpen.value = false
    await load()
  } catch (cause: any) {
    const failure = cause instanceof ManagedRequestError ? cause : null
    const status = failure?.status || cause?.response?.status
    if (status === 409) revisionConflict.value = true
    const fields = failure?.fields || cause?.response?.data?.data?.fields || cause?.response?.data?.fields
    if (fields && typeof fields === 'object') fieldErrors.value = fields
    formError.value = failure?.message || cause?.response?.data?.message || cause?.message || '规则集保存失败。'
  } finally {
    saving.value = false
  }
}

async function reimport(item: ManagedRuleSet) {
  if (!item.source_url) return
  if (!await confirmAction({
    title: '重新导入远端规则？',
    message: `将重新读取“${item.name}”的远端来源，并覆盖 Zboard 当前保存的规则正文。`,
    confirmText: '重新导入',
    tone: 'danger',
  })) return
  importingID.value = item.id
  message.value = ''
  error.value = ''
  try {
    await managedRequest<ManagedRuleSet>(`/admin/subscription-rule-sets/${item.id}/import`, {
      method: 'POST',
      body: JSON.stringify({
        source_url: item.source_url,
        source_format: item.source_format || 'zero_rule_ir',
        expected_revision: item.revision,
      }),
    })
    message.value = '远端规则已重新导入并替换本地正文。'
    await load()
  } catch (cause: any) {
    error.value = cause?.message || '远端规则导入失败。'
  } finally {
    importingID.value = 0
  }
}

async function reimportCurrent() {
  const item = ruleSets.value.find(candidate => candidate.id === form.id)
  if (!item) return
  await reimport({ ...item, source_url: form.source_url, source_format: form.source_format, revision: form.revision })
  if (editorOpen.value) await reloadEditor()
}

async function copyEndpoint(item: ManagedRuleSet) {
  if (!item.public_url) return
  try {
    await navigator.clipboard.writeText(item.public_url)
    message.value = '规则集公开地址已复制。'
  } catch {
    error.value = '无法访问剪贴板，请手动复制规则集地址。'
  }
}

async function remove(item: ManagedRuleSet) {
  if (item.usage_count > 0 || !await confirmAction({
    title: '删除规则集',
    message: `删除“${item.name}”后，规则正文和已生成产物都会被移除且无法恢复。`,
    confirmText: '确认删除',
    tone: 'danger',
  })) return
  deletingID.value = item.id
  message.value = ''
  try {
    await deleteSubscriptionRuleSet(item.id)
    message.value = '规则集已删除。'
    await load()
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || cause?.message || '规则集删除失败。'
  } finally {
    deletingID.value = 0
  }
}

function sourceHost(value?: string) {
  if (!value) return '本地维护'
  try {
    return new URL(value).host
  } catch {
    return value
  }
}

function sourceFormatLabel(value?: string) {
  return sourceFormatOptions.find(option => option.value === value)?.label || (value ? value : '无远端来源')
}

function contentByteLength(value: string) {
  return new TextEncoder().encode(value).byteLength
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('zh-CN').format(value || 0)
}

onMounted(load)
</script>

<style scoped>
.compact-cell {
  gap: 0.2rem;
}

.rule-source-editor {
  display: grid;
  gap: 0.65rem;
}

.rule-source-toolbar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 0.55rem;
  color: var(--text-muted);
  font-size: 0.82rem;
}

.rule-source-toolbar > span {
  margin-right: auto;
}

.rule-source-editor :deep(textarea) {
  min-height: 24rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  line-height: 1.55;
  tab-size: 2;
}
</style>
