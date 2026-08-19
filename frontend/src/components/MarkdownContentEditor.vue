<template>
  <article class="markdown-editor" :aria-labelledby="`${config.config_key}-editor-title`">
    <header class="markdown-editor__header">
      <div>
        <div class="markdown-editor__title-row">
          <strong :id="`${config.config_key}-editor-title`">{{ config.name }}</strong>
          <StatusBadge :tone="remote ? 'info' : 'neutral'">{{ remote ? '远端 URL' : 'Markdown' }}</StatusBadge>
          <StatusBadge v-if="dirty" tone="warning" icon="edit">已修改</StatusBadge>
        </div>
        <p>{{ config.description }}</p>
      </div>
      <div class="markdown-editor__actions">
        <UiButton variant="ghost" size="sm" type="button" :disabled="saving || source === template" @click="restoreTemplate">恢复默认模板</UiButton>
        <UiButton v-if="conflict" variant="ghost" size="sm" type="button" :disabled="saving" @click="$emit('reload')"><UiIcon name="refresh" />重新载入</UiButton>
        <UiButton variant="secondary" size="sm" type="button" :loading="saving" :disabled="!dirty" @click="$emit('save')">保存</UiButton>
      </div>
    </header>

    <UiTabs v-model="mode" :items="tabs" :label="`${config.name}编辑模式`" />

    <div v-if="mode === 'edit'" class="markdown-editor__workspace">
      <UiTextarea
        :id="`${config.config_key}-control`"
        class="markdown-editor__textarea"
        :model-value="source"
        rows="18"
        spellcheck="false"
        :aria-describedby="`${config.config_key}-help`"
        @update:model-value="$emit('update:modelValue', String($event ?? ''))"
      />
      <div :id="`${config.config_key}-help`" class="markdown-editor__help">
        <span>支持 Markdown：标题、粗体、列表、引用、链接和行内代码。</span>
        <span>变量：<code>{{ variableHint }}</code></span>
        <span>若最终内容仅保留一行完整 HTTP/HTTPS URL，前台会自动按远端页面展示。</span>
      </div>
    </div>

    <div v-else class="markdown-editor__preview">
      <div v-if="remote" class="remote-preview">
        <UiIcon name="link" />
        <div><strong>远端页面模式</strong><p>{{ source }}</p></div>
        <a :href="source" target="_blank" rel="noreferrer">打开检查</a>
      </div>
      <div v-else class="policy-markdown" v-html="previewHtml"></div>
    </div>

    <p v-if="error" class="markdown-editor__error" role="alert"><UiIcon name="alert" />{{ error }}</p>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SystemConfig } from '../api/client'
import type { SiteProfile } from '../utils/siteProfile'
import { isRemoteLegalContent, renderSafeMarkdown, resolveLegalVariables } from '../utils/legalContent'
import StatusBadge from './StatusBadge.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiTabs from './UiTabs.vue'
import UiTextarea from './UiTextarea.vue'

const props = defineProps<{
  config: SystemConfig
  modelValue?: unknown
  template: string
  profile: SiteProfile
  saving?: boolean
  dirty?: boolean
  error?: string
  conflict?: boolean
}>()
defineEmits<{ 'update:modelValue': [value: string]; save: []; reload: [] }>()

const mode = ref('edit')
const tabs = [
  { value: 'edit', label: '编辑', icon: 'edit' },
  { value: 'preview', label: '预览', icon: 'view' },
]
const variableHint = '{{site_name}} · {{site_url}} · {{copyright}} · {{support_email}}'
const source = computed(() => String(props.modelValue ?? ''))
const remote = computed(() => isRemoteLegalContent(source.value))
const previewHtml = computed(() => renderSafeMarkdown(resolveLegalVariables(source.value, props.profile)))

function restoreTemplate() {
  const emit = defineEmits
  void emit
}
</script>

<style scoped>
.markdown-editor { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); overflow: hidden; }
.markdown-editor__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; padding: 16px 18px; border-bottom: 1px solid var(--line); }
.markdown-editor__title-row { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; }
.markdown-editor__header strong { font-size: 13px; }
.markdown-editor__header p { margin: 5px 0 0; max-width: 720px; color: var(--muted); font-size: 10px; line-height: 1.5; }
.markdown-editor__actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
.markdown-editor :deep(.ui-tabs) { border-bottom: 1px solid var(--line); }
.markdown-editor__workspace { padding: 16px 18px 18px; }
.markdown-editor__textarea { width: 100%; min-height: 360px; font-family: var(--font-mono); font-size: 11px; line-height: 1.65; resize: vertical; }
.markdown-editor__help { display: flex; flex-wrap: wrap; gap: 6px 18px; margin-top: 9px; color: var(--muted); font-size: 9px; line-height: 1.45; }
.markdown-editor__help code { color: var(--code-text); }
.markdown-editor__preview { min-height: 360px; padding: 22px 24px; background: var(--surface-soft); }
.remote-preview { min-height: 260px; display: grid; grid-template-columns: auto minmax(0,1fr) auto; align-items: center; gap: 12px; padding: 20px; border: 1px dashed var(--line); border-radius: 10px; background: var(--surface); }
.remote-preview p { margin: 4px 0 0; color: var(--muted); overflow-wrap: anywhere; }
.remote-preview a { color: var(--primary); font-size: 11px; }
.policy-markdown { max-width: 760px; margin: 0 auto; color: var(--text); font-size: 12px; line-height: 1.75; }
.policy-markdown :deep(h1) { margin: 0 0 22px; font-size: 24px; }
.policy-markdown :deep(h2) { margin: 24px 0 8px; font-size: 16px; }
.policy-markdown :deep(h3) { margin: 18px 0 6px; font-size: 14px; }
.policy-markdown :deep(p), .policy-markdown :deep(ul), .policy-markdown :deep(ol), .policy-markdown :deep(blockquote) { margin: 9px 0; }
.policy-markdown :deep(a) { color: var(--primary); }
.policy-markdown :deep(code) { padding: 2px 4px; border-radius: 4px; background: var(--surface); font-family: var(--font-mono); }
.policy-markdown :deep(hr) { margin: 24px 0; border: 0; border-top: 1px solid var(--line); }
.markdown-editor__error { display: flex; gap: 5px; margin: 0; padding: 10px 18px; border-top: 1px solid var(--line); color: var(--danger); font-size: 10px; }
@media (max-width: 760px) { .markdown-editor__header { flex-direction: column; }.markdown-editor__actions { justify-content: flex-start; }.remote-preview { grid-template-columns: auto 1fr; }.remote-preview a { grid-column: 2; } }
</style>
