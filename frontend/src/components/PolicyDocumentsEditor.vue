<template>
  <div class="policy-documents-editor">
    <div class="policy-documents-toolbar">
      <div>
        <strong>政策文档中心</strong>
        <p>创建任意政策文档，并决定它是否出现在页脚、登录注册或购买确认区域。</p>
      </div>
      <div class="policy-documents-toolbar__actions">
        <span>{{ documents.length }} / {{ maxDocuments }} 篇</span>
        <UiButton variant="secondary" size="sm" type="button" :disabled="saving || documents.length >= maxDocuments" @click="addDocument"><UiIcon name="plus" />新建文档</UiButton>
      </div>
    </div>

    <div v-if="documents.length" class="policy-document-list">
      <article v-for="(document, index) in documents" :key="`${document.slug}-${index}`" class="policy-document-card">
        <header class="policy-document-card__header">
          <div class="policy-document-card__identity">
            <StatusBadge :tone="document.published ? 'success' : 'warning'">{{ document.published ? '已发布' : '草稿' }}</StatusBadge>
            <div><strong>{{ document.title || `未命名文档 ${index + 1}` }}</strong><small>/docs/{{ document.slug || 'new-document' }}</small></div>
          </div>
          <div class="policy-document-card__actions">
            <UiButton variant="ghost" size="sm" type="button" title="上移" aria-label="上移文档" :disabled="saving || index === 0" @click="moveDocument(index, -1)"><UiIcon name="arrow-up" /></UiButton>
            <UiButton variant="ghost" size="sm" type="button" title="下移" aria-label="下移文档" :disabled="saving || index === documents.length - 1" @click="moveDocument(index, 1)"><UiIcon name="arrow-down" /></UiButton>
            <UiButton variant="ghost" size="sm" type="button" :disabled="saving" @click="removeDocument(index)"><UiIcon name="close" />移除</UiButton>
          </div>
        </header>

        <div class="policy-document-fields form-grid">
          <FormField label="文档标题" :name="`policy-document-${index}-title`" hint="公开页面、导航与购买提示中显示的名称。" required>
            <template #default="{ controlAttrs }"><UiInput v-bind="controlAttrs" :model-value="document.title" maxlength="160" @update:model-value="updateDocument(index, 'title', String($event ?? ''))" /></template>
          </FormField>
          <FormField label="链接路径" :name="`policy-document-${index}-slug`" hint="仅小写字母、数字和连字符，保存后地址为 /docs/路径。" required>
            <template #default="{ controlAttrs }"><UiInput v-bind="controlAttrs" :model-value="document.slug" placeholder="fair-use" maxlength="80" @update:model-value="updateDocument(index, 'slug', normalizeSlug(String($event ?? '')))" /></template>
          </FormField>
          <FormField label="摘要（可选）" :name="`policy-document-${index}-summary`" hint="用于文档目录和正文标题下方，最多 512 个 UTF-8 字节。" full>
            <template #default="{ controlAttrs }"><UiTextarea v-bind="controlAttrs" :model-value="document.summary" rows="2" @update:model-value="updateDocument(index, 'summary', String($event ?? ''))" /></template>
          </FormField>
          <FormField label="正文或远端地址" :name="`policy-document-${index}-content`" hint="支持 Markdown 和 {{site_name}}、{{support_contact}}、{{copyright}}；仅填写完整 HTTP/HTTPS URL 时使用远端页面模式。" full required>
            <template #default="{ controlAttrs }"><UiTextarea v-bind="controlAttrs" class="policy-document-content" :model-value="document.content" rows="14" spellcheck="false" @update:model-value="updateDocument(index, 'content', String($event ?? ''))" /></template>
          </FormField>
        </div>

        <footer class="policy-document-options">
          <label class="policy-option policy-option-publish"><UiCheckbox :model-value="document.published" role="switch" @update:model-value="updateDocument(index, 'published', Boolean($event))" /><span><strong>发布文档</strong><small>关闭后公开目录和直接访问都会隐藏。</small></span></label>
          <fieldset>
            <legend>展示位置</legend>
            <label v-for="placement in placementOptions" :key="placement.value" class="policy-option"><UiCheckbox :model-value="document.placements.includes(placement.value)" @update:model-value="togglePlacement(index, placement.value, Boolean($event))" /><span>{{ placement.label }}</span></label>
          </fieldset>
        </footer>
      </article>
    </div>

    <div v-else class="policy-documents-empty">
      <UiIcon name="audit" />
      <div><strong>还没有政策文档</strong><p>新建服务条款、隐私政策、退款规则或公平使用政策，并按需分配展示位置。</p></div>
    </div>

    <div class="policy-documents-actions">
      <p v-if="error" class="policy-documents-error" role="alert"><UiIcon name="alert" />{{ error }}</p>
      <span></span>
      <UiButton v-if="conflict" variant="ghost" type="button" :disabled="saving" @click="emit('reload')"><UiIcon name="refresh" />重新载入</UiButton>
      <UiButton type="button" :loading="saving" :disabled="!dirty" @click="emit('save')">保存政策文档</UiButton>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { PolicyDocumentPlacement, SitePolicyDocument } from '../utils/siteProfile'
import FormField from './FormField.vue'
import StatusBadge from './StatusBadge.vue'
import UiButton from './UiButton.vue'
import UiCheckbox from './UiCheckbox.vue'
import UiIcon from './UiIcon.vue'
import UiInput from './UiInput.vue'
import UiTextarea from './UiTextarea.vue'

const props = withDefaults(defineProps<{
  modelValue?: unknown
  fallbackDocuments?: SitePolicyDocument[]
  dirty?: boolean
  saving?: boolean
  error?: string
  conflict?: boolean
}>(), {
  fallbackDocuments: () => [],
  dirty: false,
  saving: false,
  error: '',
  conflict: false,
})

const emit = defineEmits<{ 'update:modelValue': [value: string]; save: []; reload: [] }>()
const maxDocuments = 32
const documents = ref<SitePolicyDocument[]>([])
const placementOptions: Array<{ value: PolicyDocumentPlacement; label: string }> = [
  { value: 'footer', label: '页脚' },
  { value: 'auth', label: '登录与注册' },
  { value: 'purchase', label: '购买确认' },
]

function cloneDocuments(value: SitePolicyDocument[]) {
  return value.map(document => ({ ...document, placements: [...document.placements] }))
}

function parseDocuments(value: unknown): SitePolicyDocument[] {
  let source = value
  if (typeof source === 'string') {
    if (!source.trim()) return cloneDocuments(props.fallbackDocuments)
    try { source = JSON.parse(source) } catch { return [] }
  }
  if (source === null || source === undefined) return cloneDocuments(props.fallbackDocuments)
  if (!Array.isArray(source)) return []
  return source.flatMap(item => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return []
    const row = item as Record<string, unknown>
    const placements = Array.isArray(row.placements)
      ? row.placements.filter((placement): placement is PolicyDocumentPlacement => placementOptions.some(option => option.value === placement))
      : []
    return [{
      slug: typeof row.slug === 'string' ? row.slug : '',
      title: typeof row.title === 'string' ? row.title : '',
      summary: typeof row.summary === 'string' ? row.summary : '',
      content: typeof row.content === 'string' ? row.content : '',
      published: row.published !== false,
      placements: [...new Set(placements)],
    }]
  })
}

watch([() => props.modelValue, () => props.fallbackDocuments], () => {
  const next = parseDocuments(props.modelValue)
  if (JSON.stringify(next) !== JSON.stringify(documents.value)) documents.value = next
}, { immediate: true, deep: true })

function publish() {
  emit('update:modelValue', JSON.stringify(documents.value, null, 2))
}

function normalizeSlug(value: string) {
  return value.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '')
}

function uniqueSlug() {
  const used = new Set(documents.value.map(document => document.slug))
  let suffix = documents.value.length + 1
  while (used.has(`new-policy-${suffix}`)) suffix += 1
  return `new-policy-${suffix}`
}

function addDocument() {
  if (documents.value.length >= maxDocuments) return
  const slug = uniqueSlug()
  documents.value.push({ slug, title: '新政策文档', summary: '', content: '# 新政策文档\n\n请在这里填写政策正文。', published: false, placements: [] })
  publish()
}

function removeDocument(index: number) {
  documents.value.splice(index, 1)
  publish()
}

function moveDocument(index: number, direction: -1 | 1) {
  const target = index + direction
  if (target < 0 || target >= documents.value.length) return
  const [document] = documents.value.splice(index, 1)
  if (!document) return
  documents.value.splice(target, 0, document)
  publish()
}

function updateDocument<K extends keyof SitePolicyDocument>(index: number, key: K, value: SitePolicyDocument[K]) {
  const document = documents.value[index]
  if (!document) return
  document[key] = value
  publish()
}

function togglePlacement(index: number, placement: PolicyDocumentPlacement, enabled: boolean) {
  const document = documents.value[index]
  if (!document) return
  const next = new Set(document.placements)
  if (enabled) next.add(placement)
  else next.delete(placement)
  document.placements = placementOptions.map(option => option.value).filter(value => next.has(value))
  publish()
}
</script>

<style scoped>
.policy-documents-editor { padding: 16px; }
.policy-documents-toolbar,.policy-document-card__header,.policy-document-options,.policy-documents-actions { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
.policy-documents-toolbar { margin-bottom: 14px; }.policy-documents-toolbar > div:first-child { display: grid; gap: 4px; }.policy-documents-toolbar strong { font-size: 13px; }.policy-documents-toolbar p { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.5; }
.policy-documents-toolbar__actions { display: flex; align-items: center; gap: 10px; white-space: nowrap; }.policy-documents-toolbar__actions > span { color: var(--muted); font-size: 10px; }
.policy-document-list { display: grid; gap: 14px; }.policy-document-card { overflow: hidden; border: 1px solid var(--line); border-radius: 11px; background: var(--surface-soft); }
.policy-document-card__header { padding: 12px 14px; border-bottom: 1px solid var(--line); background: var(--surface); }.policy-document-card__identity,.policy-document-card__actions { display: flex; align-items: center; gap: 8px; }.policy-document-card__identity > div { display: grid; gap: 2px; }.policy-document-card__identity strong { font-size: 12px; }.policy-document-card__identity small { color: var(--muted); font-size: 9px; }
.policy-document-fields { padding: 14px; }.policy-document-content { font-family: var(--font-mono); font-size: 11px; line-height: 1.65; }
.policy-document-options { align-items: stretch; padding: 12px 14px; border-top: 1px solid var(--line); background: var(--surface); }.policy-document-options fieldset { display: flex; align-items: center; gap: 14px; margin: 0; padding: 0; border: 0; }.policy-document-options legend { float: left; margin-right: 12px; color: var(--muted); font-size: 10px; }
.policy-option { display: flex; align-items: center; gap: 7px; color: var(--text); font-size: 10px; }.policy-option-publish span { display: grid; gap: 2px; }.policy-option-publish strong { font-size: 10px; }.policy-option-publish small { color: var(--muted); font-size: 9px; }
.policy-documents-empty { display: flex; align-items: flex-start; gap: 11px; padding: 20px; border: 1px dashed var(--line); border-radius: 10px; background: var(--surface-soft); color: var(--muted); }.policy-documents-empty > :first-child { color: var(--primary); font-size: 20px; }.policy-documents-empty strong { color: var(--text); font-size: 12px; }.policy-documents-empty p { margin: 4px 0 0; font-size: 10px; }
.policy-documents-actions { margin-top: 14px; justify-content: flex-end; }.policy-documents-actions > span { flex: 1; }.policy-documents-error { display: flex; align-items: flex-start; gap: 5px; margin: 0 auto 0 0; color: var(--danger); font-size: 10px; }
@media (max-width: 760px) { .policy-documents-toolbar,.policy-document-card__header,.policy-document-options { align-items: stretch; flex-direction: column; }.policy-document-card__actions { flex-wrap: wrap; }.policy-document-options fieldset { flex-wrap: wrap; }.policy-documents-actions { flex-wrap: wrap; }.policy-documents-actions > span { display: none; } }
</style>
