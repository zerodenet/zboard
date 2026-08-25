<template>
  <div class="email-template-manager stack-lg">
    <PageAlert tone="info" title="可用模板变量">
      <span class="mono">{{ variableHelp }}</span>。发送运营任务时按每位收件人替换；注册通知在账户创建后进入可重试任务队列。
    </PageAlert>

    <PageAlert v-if="error" tone="danger" title="邮件模板操作失败">{{ error }}</PageAlert>

    <section class="template-block" aria-labelledby="registration-email-title">
      <header>
        <div><h3 id="registration-email-title">注册通知模板</h3><p>系统固定触发器；启用后，新用户注册成功会自动创建一条邮件任务。</p></div>
      </header>
      <div v-if="registrationTemplate" class="template-card template-card-registration">
        <div class="template-card-copy">
          <div class="template-title"><strong>{{ registrationTemplate.name }}</strong><StatusBadge :tone="registrationTemplate.is_active ? 'success' : 'neutral'">{{ registrationTemplate.is_active ? '已启用' : '未启用' }}</StatusBadge></div>
          <p>{{ registrationTemplate.subject_template }}</p>
          <small>触发器：用户注册成功 · 修订 #{{ registrationTemplate.revision }}</small>
        </div>
        <div class="template-actions">
          <UiButton variant="ghost" type="button" @click="openEditor(registrationTemplate)"><UiIcon name="edit" />编辑</UiButton>
          <UiButton :variant="registrationTemplate.is_active ? 'danger' : 'secondary'" type="button" :loading="statusChangingID === registrationTemplate.id" @click="toggleActive(registrationTemplate)">{{ registrationTemplate.is_active ? '停用' : '启用' }}</UiButton>
        </div>
      </div>
      <EmptyState v-else-if="!loading" icon="audit" title="注册通知模板不可用" description="系统尚未创建注册通知模板，请检查数据库迁移状态。" />
    </section>

    <section class="template-block" aria-labelledby="operational-email-title">
      <header>
        <div><h3 id="operational-email-title">运营模板</h3><p>维护公告、活动通知等可复用内容；在运营任务中选择后会复制为任务快照。</p></div>
        <UiButton type="button" @click="openCreate"><UiIcon name="plus" />新增运营模板</UiButton>
      </header>
      <div v-if="operationalTemplates.length" class="template-grid">
        <article v-for="template in operationalTemplates" :key="template.id" class="template-card">
          <div class="template-card-copy">
            <div class="template-title"><strong>{{ template.name }}</strong><StatusBadge :tone="template.is_active ? 'success' : 'neutral'">{{ template.is_active ? '可选' : '已停用' }}</StatusBadge></div>
            <p>{{ template.subject_template }}</p>
            <small><span class="mono">{{ template.slug }}</span> · 排序 {{ template.sort_order }} · 修订 #{{ template.revision }}</small>
          </div>
          <div class="template-actions">
            <UiButton variant="ghost" type="button" @click="openEditor(template)"><UiIcon name="edit" />编辑</UiButton>
            <UiButton variant="ghost" type="button" :loading="statusChangingID === template.id" @click="toggleActive(template)">{{ template.is_active ? '停用' : '启用' }}</UiButton>
            <UiButton variant="danger" type="button" :loading="deletingID === template.id" @click="removeTemplate(template)">删除</UiButton>
          </div>
        </article>
      </div>
      <EmptyState v-else-if="!loading" icon="audit" title="没有运营模板" description="新增模板后，可在运营任务中直接套用并继续编辑本次发送内容。" />
      <p v-if="loading" class="loading-copy">正在加载邮件模板…</p>
    </section>

    <ModalDialog :open="editorOpen" :title="form.id ? '编辑邮件模板' : '新增运营模板'" :description="form.category === 'registration' ? '注册触发器与模板标识由系统固定。' : '保存后可在运营任务中复用。'" size="lg" :busy="saving || previewing" :dirty="editorDirty" @close="closeEditor">
      <form ref="formElement" class="stack" novalidate @submit.prevent="saveTemplate">
        <div class="form-grid">
          <FormField v-slot="{ controlAttrs }" label="模板名称" name="email-template-name" :error="editorErrors.fields.name" required><UiInput v-model="form.name" v-bind="controlAttrs" maxlength="80" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="模板标识" name="email-template-slug" hint="仅用于管理识别，不会暴露为公开链接。" :error="editorErrors.fields.slug" required><UiInput v-model="form.slug" v-bind="controlAttrs" maxlength="80" :disabled="form.category === 'registration'" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="排序" name="email-template-order" :error="editorErrors.fields.sort_order"><UiNumberInput v-model="form.sort_order" v-bind="controlAttrs" /></FormField>
          <label class="template-switch"><span><strong>启用模板</strong><small>{{ form.category === 'registration' ? '注册后自动排队发送' : '允许运营任务选择' }}</small></span><UiCheckbox v-model="form.is_active" role="switch" /></label>
        </div>
        <FormField v-slot="{ controlAttrs }" label="邮件主题" name="email-template-subject" :error="editorErrors.fields.subject_template" required full><UiInput v-model="form.subject_template" v-bind="controlAttrs" maxlength="200" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="纯文本正文" name="email-template-body" hint="支持上方列出的变量；实际发送前按收件人替换。" :error="editorErrors.fields.body_template" required full><UiTextarea v-model="form.body_template" v-bind="controlAttrs" maxlength="100000" rows="12" /></FormField>
        <PageAlert v-if="editorErrors.formError.value" tone="danger" title="邮件模板尚未保存">{{ editorErrors.formError.value }}</PageAlert>
        <PageAlert v-if="revisionConflict" tone="warning" title="模板已发生变化">另一位管理员已经保存了新版本，请重新载入后再编辑。</PageAlert>
        <div v-if="preview" class="email-preview">
          <small>示例预览</small>
          <strong>{{ preview.subject }}</strong>
          <pre>{{ preview.body }}</pre>
        </div>
      </form>
      <template #footer="{ requestClose }">
        <div class="dialog-actions">
          <UiButton variant="ghost" type="button" :disabled="saving" @click="requestClose">取消</UiButton>
          <UiButton variant="secondary" type="button" :loading="previewing" @click="runPreview"><UiIcon name="info" />生成预览</UiButton>
          <UiButton type="button" :loading="saving" @click="saveTemplate">保存模板</UiButton>
        </div>
      </template>
    </ModalDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createEmailTemplate,
  deleteEmailTemplate,
  fetchEmailTemplates,
  previewEmailTemplate,
  updateEmailTemplate,
  type EmailTemplate,
  type EmailTemplateCategory,
  type EmailTemplatePreview,
} from '../api/client'
import { useDirtyForm, useFormErrors } from '../composables/useFormState'
import { confirmAction, notify } from '../utils/feedback'
import { collectFieldErrors, isIntegerInRange, isSlug, isUtf8LengthInRange } from '../utils/validation'
import EmptyState from './EmptyState.vue'
import FormField from './FormField.vue'
import ModalDialog from './ModalDialog.vue'
import PageAlert from './PageAlert.vue'
import StatusBadge from './StatusBadge.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'
import UiNumberInput from './UiNumberInput.vue'
import UiTextarea from './UiTextarea.vue'

const variableHelp = '{{site_name}}、{{site_url}}、{{user_email}}、{{account_name}}、{{registered_at}}、{{current_date}}'
const emit = defineEmits<{ dirty: [value: boolean] }>()
const emptyForm = () => ({ id: 0, name: '', slug: '', category: 'operational' as EmailTemplateCategory, subject_template: '', body_template: '', is_active: true, sort_order: 0, revision: 0 })
const templates = ref<EmailTemplate[]>([])
const loading = ref(false)
const saving = ref(false)
const previewing = ref(false)
const editorOpen = ref(false)
const deletingID = ref(0)
const statusChangingID = ref(0)
const error = ref('')
const revisionConflict = ref(false)
const preview = ref<EmailTemplatePreview | null>(null)
const formElement = ref<HTMLElement | null>(null)
const form = reactive(emptyForm())
const editorState = useDirtyForm(() => form)
const editorDirty = computed(() => editorState.dirty.value)
const editorErrors = useFormErrors()
const registrationTemplate = computed(() => templates.value.find(item => item.category === 'registration'))
const operationalTemplates = computed(() => templates.value.filter(item => item.category === 'operational').sort((a, b) => a.sort_order - b.sort_order || a.id - b.id))

async function loadTemplates() {
  loading.value = true; error.value = ''
  try { templates.value = await fetchEmailTemplates() }
  catch (cause: any) { error.value = cause?.response?.data?.message || '邮件模板加载失败。' }
  finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, emptyForm()); editorErrors.clear(); preview.value = null; revisionConflict.value = false
  editorState.markClean(); editorOpen.value = true
}
function openEditor(template: EmailTemplate) {
  Object.assign(form, template); editorErrors.clear(); preview.value = null; revisionConflict.value = false
  editorState.markClean(); editorOpen.value = true
}
function closeEditor() { if (saving.value || previewing.value) return; editorOpen.value = false; preview.value = null }
async function validateForm() {
  editorErrors.clear()
  form.name = form.name.trim(); form.slug = form.slug.trim().toLowerCase(); form.subject_template = form.subject_template.trim()
  return editorErrors.applyValidation(collectFieldErrors({
    name: !isUtf8LengthInRange(form.name, 1, 80, true) && '模板名称需包含 1 到 80 个 UTF-8 字节。',
    slug: !isSlug(form.slug, 80) && '模板标识只能包含小写字母、数字和单个连字符。',
    subject_template: (!isUtf8LengthInRange(form.subject_template, 1, 200, true) || /[\r\n]/.test(form.subject_template)) && '邮件主题需包含 1 到 200 个 UTF-8 字节，且不能换行。',
    body_template: !isUtf8LengthInRange(form.body_template, 1, 100000, true) && '邮件正文需包含 1 到 100000 个 UTF-8 字节。',
    sort_order: !isIntegerInRange(form.sort_order, Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER) && '排序必须为整数。',
  }), formElement, '请更正标记字段后再保存模板。')
}
function payload() {
  return { name: form.name, slug: form.slug, category: form.category, subject_template: form.subject_template, body_template: form.body_template, is_active: form.is_active, sort_order: Number(form.sort_order || 0), ...(form.id ? { expected_revision: form.revision } : {}) }
}
async function runPreview() {
  if (!await validateForm()) return
  previewing.value = true
  try { preview.value = await previewEmailTemplate(payload()) }
  catch (cause: any) {
    await editorErrors.applyApiError(cause, '邮件预览生成失败。', formElement)
  } finally { previewing.value = false }
}
async function saveTemplate() {
  if (!await validateForm()) return
  saving.value = true; error.value = ''
  try {
    if (form.id) await updateEmailTemplate(form.id, payload()); else await createEmailTemplate(payload())
    editorState.markClean(); editorOpen.value = false; preview.value = null; revisionConflict.value = false
    notify('邮件模板已保存', '模板内容和启用状态已更新。', 'success'); await loadTemplates()
  } catch (cause: any) {
    if (cause?.response?.status === 409) revisionConflict.value = true
    else {
      await editorErrors.applyApiError(cause, '邮件模板保存失败。', formElement)
    }
  } finally { saving.value = false }
}
async function toggleActive(template: EmailTemplate) {
  if (template.is_active && !await confirmAction({ title: '停用邮件模板', message: template.category === 'registration' ? '停用后，新注册用户不会再收到欢迎通知。' : `停用后，“${template.name}”将不再出现在运营任务的可选模板中。`, confirmText: '确认停用', tone: 'danger' })) return
  statusChangingID.value = template.id; error.value = ''
  try {
    await updateEmailTemplate(template.id, { name: template.name, slug: template.slug, category: template.category, subject_template: template.subject_template, body_template: template.body_template, is_active: !template.is_active, sort_order: template.sort_order, expected_revision: template.revision })
    notify(template.is_active ? '邮件模板已停用' : '邮件模板已启用', template.name, 'success'); await loadTemplates()
  } catch (cause: any) { error.value = cause?.response?.data?.message || '模板状态更新失败。' }
  finally { statusChangingID.value = 0 }
}
async function removeTemplate(template: EmailTemplate) {
  if (!await confirmAction({ title: '删除运营模板', message: `删除“${template.name}”不会影响已经创建的邮件任务快照。`, confirmText: '确认删除', tone: 'danger' })) return
  deletingID.value = template.id; error.value = ''
  try { await deleteEmailTemplate(template.id); notify('运营模板已删除', template.name, 'success'); await loadTemplates() }
  catch (cause: any) { error.value = cause?.response?.data?.message || '运营模板删除失败。' }
  finally { deletingID.value = 0 }
}

for (const [source, field] of [
  [() => form.name, 'name'], [() => form.slug, 'slug'], [() => form.subject_template, 'subject_template'],
  [() => form.body_template, 'body_template'], [() => form.sort_order, 'sort_order'],
] as Array<[() => unknown, string]>) watch(source, () => editorErrors.clear(field))
watch(() => editorOpen.value && editorDirty.value, value => emit('dirty', value), { immediate: true })

onMounted(loadTemplates)
</script>

<style scoped>
.email-template-manager{padding:16px}.template-block{display:grid;gap:12px}.template-block>header{display:flex;align-items:flex-end;justify-content:space-between;gap:16px}.template-block h3{margin:0;font-size:14px}.template-block header p{margin:4px 0 0;color:var(--muted);font-size:11px}.template-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.template-card{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:14px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}.template-card-registration{background:var(--surface)}.template-card-copy{min-width:0;display:grid;gap:6px}.template-title{display:flex;align-items:center;gap:8px}.template-card p,.template-card small{margin:0}.template-card p{font-size:12px}.template-card small{color:var(--muted);font-size:10px}.template-actions,.dialog-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}.template-switch{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 12px;border:1px solid var(--line);border-radius:9px}.template-switch span{display:grid;gap:2px}.template-switch small,.loading-copy{color:var(--muted);font-size:10px}.email-preview{display:grid;gap:8px;padding:14px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}.email-preview small{color:var(--muted)}.email-preview pre{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;font-size:12px;line-height:1.65}@media(max-width:760px){.template-grid{grid-template-columns:1fr}.template-block>header,.template-card{align-items:stretch;flex-direction:column}.template-actions{justify-content:flex-start}}
</style>
