<template>
  <section class="standard-page">
    <PageHeader title="规则集" description="由 Zboard 维护、校验并发布 Zero Rule IR；订阅模板只引用稳定规则地址并选择命中后的路由动作。" eyebrow="订阅模板">
      <template #actions>
        <PageRefreshButton label="刷新规则集" :loading="loading" @click="load" />
        <UiButton type="button" @click="openCreate"><UiIcon name="plus" />新建规则集</UiButton>
      </template>
    </PageHeader>

    <SubscriptionTemplateSectionNav section="rule-sets" />

    <PageAlert tone="info" title="统一规则源">
      正文固定保存为 Zero Rule IR v1。远端地址只用于导入，模板会收到 Zboard 为 znet-sink、Clash 或 sing-box 生成的规则地址。
    </PageAlert>

    <TransientFeedback :success="message" :error="feedbackError" success-title="规则集操作已完成" error-title="规则集操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(search || activeFilter)" @clear="resetFilters">
          <WorkbenchFilterInput v-model="search" label="搜索" placeholder="名称、标识、说明或来源地址" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="activeFilter" label="可用状态" :options="activeOptions" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>

      <DataTable v-if="ruleSets.length" caption="Zboard 自有规则集列表" :row-count="total" :min-width="1080" table-class="subscription-rule-set-table">
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
                <span><code>{{ item.tag }}</code><template v-if="item.public_url"> · {{ sourceHost(item.public_url) }}</template></span>
              </div>
            </td>
            <td>
              <StatusBadge :tone="item.source_url ? 'info' : 'neutral'" :icon="item.source_url ? 'refresh' : 'edit'">
                {{ item.source_url ? sourceFormatLabel(item.source_format) : '面板维护' }}
              </StatusBadge>
            </td>
            <td class="numeric-column">{{ formatNumber(item.rule_count) }}</td>
            <td data-column-priority="2">{{ formatBytes(item.content_bytes) }}</td>
            <td>
              <StatusBadge :tone="item.is_active ? 'success' : 'neutral'" :icon="item.is_active ? 'check' : 'minus'">
                {{ item.is_active ? '可供选择' : '已停用' }}
              </StatusBadge>
            </td>
            <td class="numeric-column">{{ item.usage_count }}</td>
            <td data-column-priority="2"><TimeBadge :value="item.updated_at" /></td>
            <td class="table-action-column">
              <RowActions :label="`${item.name} 的操作`" :trigger-key="`subscription-rule-set-${item.id}`">
                <UiButton variant="secondary" size="sm" type="button" :loading="editingID === item.id" @click="openEdit(item)">
                  <UiIcon name="edit" />编辑正文
                </UiButton>
                <UiButton v-if="item.source_url" variant="secondary" size="sm" type="button" :loading="syncingID === item.id" @click="syncRemote(item)">
                  <UiIcon name="refresh" />同步远端
                </UiButton>
                <UiButton v-if="item.public_url" variant="secondary" size="sm" type="button" @click="copyPublicURL(item)">复制地址</UiButton>
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
        :description="search || activeFilter ? '调整或清除筛选条件后重试。' : '创建规则集后，Zboard 会保存规范化的 Zero Rule IR，并按客户端格式发布稳定地址。'"
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
      :dirty="editorDirty"
      :title="form.id ? '编辑规则集' : '新建规则集'"
      description="规则正文只描述匹配条件；代理、直连或拦截动作由订阅模板单独配置。"
      size="lg"
      :busy="saving"
      :return-focus-selector="form.id ? `[data-row-action-trigger='subscription-rule-set-${form.id}']` : ''"
      @close="closeEditor"
    >
      <form id="managed-rule-set-form" class="rule-set-editor" novalidate @submit.prevent="save">
        <div v-if="form.id" class="editor-meta">
          <StatusBadge tone="neutral" icon="history">版本 {{ form.revision }}</StatusBadge>
          <StatusBadge tone="info" icon="audit">{{ formatNumber(form.rule_count) }} 条规则</StatusBadge>
          <StatusBadge tone="neutral" icon="database">{{ formatBytes(form.content_bytes) }}</StatusBadge>
          <TimeBadge :value="form.updated_at" />
        </div>

        <PageAlert v-if="revisionConflict" tone="warning" title="规则集已被其他管理员更新">
          当前草稿基于旧版本，请重新加载最新正文后继续编辑。
          <template #actions>
            <UiButton variant="secondary" size="sm" type="button" :loading="editingID === form.id" @click="reloadEditor">
              <UiIcon name="refresh" />重新加载
            </UiButton>
          </template>
        </PageAlert>
        <PageAlert v-if="editorError" tone="danger" title="无法保存规则集">{{ editorError }}</PageAlert>

        <div class="form-grid">
          <FormField label="规则集名称" name="managed-rule-set-name" :error="fieldErrors.name" hint="用于后台检索和模板选择。" required>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.name" v-bind="controlAttrs" maxlength="80" placeholder="例如：广告拦截" /></template>
          </FormField>

          <FormField label="规则标识" name="managed-rule-set-tag" :error="fieldErrors.tag" :hint="form.id ? '用于公开地址，创建后不能修改。' : '仅允许字母、数字、点、下划线和连字符。'" required>
            <template #default="{ controlAttrs }"><UiInput v-model.trim="form.tag" v-bind="controlAttrs" maxlength="64" :disabled="Boolean(form.id)" placeholder="例如：reject-ads" /></template>
          </FormField>

          <FormField label="维护方式" name="managed-rule-set-mode" hint="远端内容会导入 Zboard，不会在模板中直接透传第三方地址。" required>
            <template #default="{ controlAttrs }"><UiSelect v-model="form.mode" v-bind="controlAttrs" :options="modeOptions" /></template>
          </FormField>

          <FormField label="客户端下载间隔" name="managed-rule-set-interval" :error="fieldErrors.sync_interval" hint="写入订阅配置，60 秒至 7 天。" required>
            <template #default="{ controlAttrs }"><UiNumberInput v-model="form.sync_interval" v-bind="controlAttrs" :min="60" :max="604800" suffix=" 秒" /></template>
          </FormField>

          <template v-if="form.mode === 'remote'">
            <FormField label="远端来源格式" name="managed-rule-set-source-format" :error="fieldErrors.source_format" hint="导入后统一转换为 Zero Rule IR v1。" required>
              <template #default="{ controlAttrs }"><UiSelect v-model="form.source_format" v-bind="controlAttrs" :options="sourceFormatOptions" /></template>
            </FormField>

            <FormField label="远端来源地址" name="managed-rule-set-source-url" :error="fieldErrors.source_url" hint="仅支持 HTTP(S)，并阻止内网、回环和链路本地地址。" required full>
              <template #default="{ controlAttrs }"><UiInput v-model.trim="form.source_url" v-bind="controlAttrs" type="url" maxlength="2048" placeholder="https://example.com/rules/ads.json" /></template>
            </FormField>
          </template>

          <FormField label="用途说明" name="managed-rule-set-description" :error="fieldErrors.description" full>
            <template #default="{ controlAttrs }"><UiTextarea v-model.trim="form.description" v-bind="controlAttrs" rows="3" maxlength="255" placeholder="说明规则来源、覆盖范围或维护责任。" /></template>
          </FormField>

          <FormField
            v-if="form.mode === 'manual'"
            label="Zero Rule IR v1 正文"
            name="managed-rule-set-content"
            :error="fieldErrors.content"
            hint="仅接受 version、可选 name 和 rules；规则项只包含 type 与 value。保存时会规范化、排序和去重。"
            required
            full
          >
            <template #default="{ controlAttrs }">
              <UiTextarea v-model="form.content" v-bind="controlAttrs" class="rule-source-editor" rows="18" spellcheck="false" />
            </template>
          </FormField>

          <FormField label="可用状态" name="managed-rule-set-active" :error="fieldErrors.is_active" full>
            <label class="check-field">
              <UiCheckbox v-model="form.is_active" />
              <span><strong>允许模板选择</strong><br /><small class="field-hint">停用后不会出现在新选择结果中；已有模板引用仍继续生效。</small></span>
            </label>
          </FormField>
        </div>
      </form>

      <template #footer="{ requestClose }">
        <UiButton variant="secondary" type="button" :disabled="saving" @click="requestClose">取消</UiButton>
        <UiButton form="managed-rule-set-form" type="submit" :loading="saving" :disabled="revisionConflict">
          {{ form.mode === 'remote' ? '导入并保存' : '保存规则集' }}
        </UiButton>
      </template>
    </ModalDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeRouteUpdate, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  createManagedRuleSet,
  deleteManagedRuleSet,
  fetchManagedRuleSet,
  fetchManagedRuleSetContent,
  fetchManagedRuleSetsPage,
  importManagedRuleSet,
  updateManagedRuleSet,
  type ManagedRuleSet,
  type ManagedRuleSourceFormat,
} from '../api/managedRuleSets'
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
import { useRemoteTable } from '../composables/useRemoteTable'
import { confirmAction } from '../utils/feedback'

const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const activeOptions = [
  { label: '全部状态', value: '' },
  { label: '可供选择', value: 'true' },
  { label: '已停用', value: 'false' },
]
const modeOptions = [
  { label: '面板维护 Zero Rule IR', value: 'manual' },
  { label: '从远端导入并同步', value: 'remote' },
]
const sourceFormatOptions = [
  { label: 'Zero Rule IR v1', value: 'zero_rule_ir' },
  { label: '域名列表', value: 'domain_list' },
  { label: 'CIDR 列表', value: 'cidr_list' },
  { label: 'Clash classical', value: 'clash_classical' },
]
const defaultContent = `{
  "version": 1,
  "name": "example",
  "rules": [
    { "type": "domain_suffix", "value": "example.com" }
  ]
}\n`

function pageLimit(value: unknown) {
  const parsed = Number(value)
  return allowedPageSizes.includes(parsed) ? parsed : 50
}

const search = ref(String(route.query.q || ''))
const activeFilter = ref(String(route.query.active || ''))
const limit = ref(pageLimit(route.query.limit))
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const emptyForm = () => ({
  id: 0,
  name: '',
  description: '',
  tag: '',
  mode: 'manual' as 'manual' | 'remote',
  source_url: '',
  source_format: 'zero_rule_ir' as ManagedRuleSourceFormat,
  content: defaultContent,
  sync_interval: 86400,
  is_active: true,
  revision: 0,
  rule_count: 0,
  content_bytes: 0,
  usage_count: 0,
  updated_at: '',
})
const form = reactive(emptyForm())
const initialSnapshot = ref('')
const editorOpen = ref(false)
const saving = ref(false)
const editingID = ref(0)
const syncingID = ref(0)
const deletingID = ref(0)
const message = ref('')
const operationError = ref('')
const editorError = ref('')
const revisionConflict = ref(false)
const fieldErrors = reactive<Record<string, string>>({})
const editorDirty = computed(() => editorOpen.value && JSON.stringify(form) !== initialSnapshot.value)

const { items: ruleSets, total, loading, refreshing, error, load } = useRemoteTable<ManagedRuleSet>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchManagedRuleSetsPage({
    q: search.value || undefined,
    active: activeFilter.value === '' ? undefined : activeFilter.value === 'true',
    offset: offset.value,
    limit: limit.value,
  }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '规则集加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
const feedbackError = computed(() => operationError.value || error.value)

function setSnapshot() {
  initialSnapshot.value = JSON.stringify(form)
}

function clearEditorErrors() {
  editorError.value = ''
  revisionConflict.value = false
  for (const key of Object.keys(fieldErrors)) delete fieldErrors[key]
}

function resetEditorState() {
  Object.assign(form, emptyForm())
  clearEditorErrors()
  setSnapshot()
}

async function syncURL(replace = false) {
  const query: Record<string, string> = {}
  if (search.value) query.q = search.value
  if (activeFilter.value) query.active = activeFilter.value
  if (offset.value > 0) query.page = String(Math.floor(offset.value / limit.value) + 1)
  if (limit.value !== 50) query.limit = String(limit.value)
  if (replace) await router.replace({ query })
  else await router.push({ query })
}

async function applyFilters() {
  offset.value = 0
  await syncURL()
  await load()
}

async function resetFilters() {
  search.value = ''
  activeFilter.value = ''
  offset.value = 0
  await syncURL()
  await load()
}

async function changePage(next: { offset: number; limit: number }) {
  offset.value = next.offset
  limit.value = next.limit
  await syncURL()
  await load()
}

function openCreate() {
  resetEditorState()
  editorOpen.value = true
}

async function openEdit(item: ManagedRuleSet) {
  editingID.value = item.id
  operationError.value = ''
  try {
    const [latest, source] = await Promise.all([
      fetchManagedRuleSet(item.id),
      fetchManagedRuleSetContent(item.id),
    ])
    Object.assign(form, {
      id: latest.id,
      name: latest.name,
      description: latest.description || '',
      tag: latest.tag,
      mode: latest.source_url ? 'remote' : 'manual',
      source_url: latest.source_url || '',
      source_format: latest.source_format || 'zero_rule_ir',
      content: source.content || defaultContent,
      sync_interval: latest.interval,
      is_active: latest.is_active,
      revision: source.revision,
      rule_count: source.rule_count,
      content_bytes: source.content_bytes,
      usage_count: latest.usage_count,
      updated_at: latest.updated_at,
    })
    clearEditorErrors()
    setSnapshot()
    editorOpen.value = true
  } catch (cause: any) {
    operationError.value = cause?.response?.data?.message || '规则集详情加载失败。'
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
  resetEditorState()
}

function validateForm() {
  for (const key of Object.keys(fieldErrors)) delete fieldErrors[key]
  if (!form.name.trim()) fieldErrors.name = '请输入规则集名称。'
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(form.tag)) fieldErrors.tag = '规则标识仅允许字母、数字、点、下划线和连字符。'
  if (form.description.length > 255) fieldErrors.description = '用途说明不能超过 255 个字符。'
  if (!Number.isInteger(form.sync_interval) || form.sync_interval < 60 || form.sync_interval > 604800) fieldErrors.sync_interval = '下载间隔必须在 60 秒到 7 天之间。'
  if (form.mode === 'remote') {
    if (!/^https?:\/\//i.test(form.source_url)) fieldErrors.source_url = '请输入完整的 HTTP 或 HTTPS 地址。'
    if (!sourceFormatOptions.some(option => option.value === form.source_format)) fieldErrors.source_format = '请选择受支持的远端格式。'
  } else if (!form.content.trim()) {
    fieldErrors.content = '请输入 Zero Rule IR 正文。'
  }
  return Object.keys(fieldErrors).length === 0
}

async function save() {
  if (!validateForm()) return
  saving.value = true
  operationError.value = ''
  editorError.value = ''
  message.value = ''
  try {
    const base = {
      name: form.name.trim(),
      description: form.description.trim(),
      tag: form.tag.trim(),
      source_format: (form.mode === 'remote' ? form.source_format : 'zero_rule_ir') as ManagedRuleSourceFormat,
      sync_interval: form.sync_interval,
      is_active: form.is_active,
    }
    if (!form.id) {
      await createManagedRuleSet(form.mode === 'remote'
        ? { ...base, source_url: form.source_url.trim() }
        : { ...base, content: form.content })
    } else if (form.mode === 'remote') {
      const imported = await importManagedRuleSet(form.id, form.source_url.trim(), form.source_format, form.revision)
      await updateManagedRuleSet(form.id, {
        ...base,
        source_url: form.source_url.trim(),
        expected_revision: imported.revision,
      })
    } else {
      await updateManagedRuleSet(form.id, {
        ...base,
        content: form.content,
        expected_revision: form.revision,
      })
    }
    message.value = form.id ? '规则集已更新。' : '规则集已创建。'
    closeEditor()
    await load()
  } catch (cause: any) {
    const payload = cause?.response?.data
    if (Number(cause?.response?.status || 0) === 409) revisionConflict.value = true
    if (payload?.fields && typeof payload.fields === 'object') Object.assign(fieldErrors, payload.fields)
    editorError.value = payload?.message || '规则集保存失败。'
  } finally {
    saving.value = false
  }
}

async function syncRemote(item: ManagedRuleSet) {
  if (!item.source_url || !item.source_format) return
  syncingID.value = item.id
  operationError.value = ''
  message.value = ''
  try {
    await importManagedRuleSet(item.id, item.source_url, item.source_format, item.revision)
    message.value = `${item.name} 已从远端同步。`
    await load()
  } catch (cause: any) {
    operationError.value = cause?.response?.data?.message || '远端规则同步失败。'
  } finally {
    syncingID.value = 0
  }
}

async function copyPublicURL(item: ManagedRuleSet) {
  if (!item.public_url) return
  try {
    await navigator.clipboard.writeText(item.public_url)
    message.value = '规则地址已复制。'
  } catch {
    operationError.value = '无法写入剪贴板，请手动复制规则地址。'
  }
}

async function remove(item: ManagedRuleSet) {
  if (item.usage_count > 0) return
  const confirmed = await confirmAction({
    title: `删除规则集“${item.name}”？`,
    message: 'Zboard 保存的正文和派生产物会一并删除，此操作无法撤销。',
    confirmText: '删除规则集',
    tone: 'danger',
  })
  if (!confirmed) return
  deletingID.value = item.id
  operationError.value = ''
  message.value = ''
  try {
    await deleteManagedRuleSet(item.id)
    message.value = '规则集已删除。'
    await load()
  } catch (cause: any) {
    operationError.value = cause?.response?.data?.message || '规则集删除失败。'
  } finally {
    deletingID.value = 0
  }
}

function sourceHost(value?: string) {
  if (!value) return '本地规则库'
  try { return new URL(value).host }
  catch { return value }
}

function sourceFormatLabel(value?: string) {
  return sourceFormatOptions.find(option => option.value === value)?.label || value || '远端来源'
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('zh-CN').format(value || 0)
}

function formatBytes(value: number) {
  if (!value) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let amount = value
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit++
  }
  return `${amount >= 10 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`
}

onBeforeRouteUpdate(async to => {
  search.value = String(to.query.q || '')
  activeFilter.value = String(to.query.active || '')
  limit.value = pageLimit(to.query.limit)
  offset.value = (Math.max(1, Number(to.query.page) || 1) - 1) * limit.value
  await load()
})

onMounted(load)
</script>

<style scoped>
.rule-set-editor {
  display: grid;
  gap: var(--space-5);
}

.editor-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-4);
}

.rule-source-editor {
  font-family: var(--font-mono);
  line-height: 1.55;
}

.check-field {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
}

@media (max-width: 760px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
