<template>
  <ModalDialog :open="open" :title="title" size="xl" fixed-body @close="$emit('close')">
    <div class="legal-dialog-content">
      <div v-if="remote" class="legal-remote">
        <div class="legal-remote__notice">
          <span><UiIcon name="info" />内容来自外部页面</span>
          <a :href="remoteUrl" target="_blank" rel="noreferrer">新窗口打开</a>
        </div>
        <iframe
          class="legal-remote__frame"
          :src="remoteUrl"
          :title="title"
          sandbox="allow-forms allow-popups allow-scripts"
          referrerpolicy="no-referrer"
        ></iframe>
        <p class="legal-remote__fallback">若远端站点禁止嵌入页面，请使用“新窗口打开”。</p>
      </div>
      <div v-else class="legal-markdown" v-html="html"></div>
    </div>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { SiteProfile } from '../utils/siteProfile'
import { isRemoteLegalContent, renderSafeMarkdown, resolveLegalVariables } from '../utils/legalContent'
import ModalDialog from './ModalDialog.vue'
import UiIcon from './UiIcon.vue'

const props = defineProps<{
  open: boolean
  title: string
  content: string
  profile: SiteProfile
}>()
defineEmits<{ close: [] }>()

const remote = computed(() => isRemoteLegalContent(props.content))
const remoteUrl = computed(() => remote.value ? props.content.trim() : '')
const html = computed(() => renderSafeMarkdown(resolveLegalVariables(props.content, props.profile)))
</script>

<style scoped>
.legal-dialog-content { max-height: min(70vh, 760px); overflow-y: auto; }
.legal-markdown { max-width: 800px; margin: 0 auto; padding: 8px 6px 28px; color: var(--text); font-size: 13px; line-height: 1.8; }
.legal-markdown :deep(h1) { margin: 0 0 24px; font-size: 27px; line-height: 1.3; }
.legal-markdown :deep(h2) { margin: 27px 0 9px; font-size: 17px; }
.legal-markdown :deep(h3) { margin: 20px 0 7px; font-size: 14px; }
.legal-markdown :deep(p), .legal-markdown :deep(ul), .legal-markdown :deep(ol), .legal-markdown :deep(blockquote) { margin: 10px 0; }
.legal-markdown :deep(a) { color: var(--primary); }
.legal-markdown :deep(blockquote) { padding-left: 14px; border-left: 3px solid var(--line-strong); color: var(--muted); }
.legal-markdown :deep(code) { padding: 2px 5px; border-radius: 4px; background: var(--surface-soft); font-family: var(--font-mono); }
.legal-markdown :deep(hr) { margin: 28px 0; border: 0; border-top: 1px solid var(--line); }
.legal-remote { display: grid; gap: 10px; min-height: 60vh; }
.legal-remote__notice { display: flex; align-items: center; justify-content: space-between; gap: 12px; color: var(--muted); font-size: 11px; }
.legal-remote__notice span { display: flex; align-items: center; gap: 6px; }
.legal-remote__notice a { color: var(--primary); }
.legal-remote__frame { width: 100%; min-height: 58vh; border: 1px solid var(--line); border-radius: 10px; background: white; }
.legal-remote__fallback { margin: 0; color: var(--muted); font-size: 9px; }
</style>
