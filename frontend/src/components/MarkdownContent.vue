<template>
  <div class="markdown-content" v-html="html"></div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { renderSafeMarkdown } from '../utils/legalContent'

const props = defineProps<{ content: string }>()
const html = computed(() => renderSafeMarkdown(props.content || ''))
</script>

<style scoped>
.markdown-content { color: var(--text); line-height: 1.75; overflow-wrap: anywhere; }
.markdown-content :deep(h1) { margin: 0 0 18px; font-size: 24px; line-height: 1.3; }
.markdown-content :deep(h2) { margin: 22px 0 8px; font-size: 18px; }
.markdown-content :deep(h3) { margin: 18px 0 7px; font-size: 15px; }
.markdown-content :deep(p), .markdown-content :deep(ul), .markdown-content :deep(ol), .markdown-content :deep(blockquote) { margin: 9px 0; }
.markdown-content :deep(ul), .markdown-content :deep(ol) { padding-left: 22px; }
.markdown-content :deep(a) { color: var(--primary); }
.markdown-content :deep(blockquote) { padding: 8px 12px; border-left: 3px solid var(--line-strong); background: var(--surface-soft); color: var(--muted); }
.markdown-content :deep(code) { padding: 2px 5px; border-radius: 4px; background: var(--surface-soft); font-family: var(--font-mono); }
.markdown-content :deep(hr) { margin: 22px 0; border: 0; border-top: 1px solid var(--line); }
</style>
