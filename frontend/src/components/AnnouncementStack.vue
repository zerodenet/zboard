<template>
  <div v-if="visible.length" class="announcement-stack" aria-live="polite">
    <article v-for="item in visible" :key="`${item.id}:${item.revision}`" :class="`announcement-${item.severity}`">
      <div><strong>{{ item.title }}</strong><p>{{ item.content }}</p></div>
      <button v-if="item.dismissible" type="button" aria-label="关闭公告" @click="dismiss(item)">×</button>
    </article>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Announcement } from '../api/client'

const props = defineProps<{ items: Announcement[] }>()
const dismissed = ref<Record<string, boolean>>(readDismissed())
const visible = computed(() => props.items.filter(item => !dismissed.value[`${item.id}:${item.revision}`]))

function readDismissed() {
  try { return JSON.parse(localStorage.getItem('zboard.dismissedAnnouncements') || '{}') || {} } catch { return {} }
}
function dismiss(item: Announcement) {
  dismissed.value = { ...dismissed.value, [`${item.id}:${item.revision}`]: true }
  localStorage.setItem('zboard.dismissedAnnouncements', JSON.stringify(dismissed.value))
}
</script>

<style scoped>
.announcement-stack { position: fixed; z-index: 1200; top: 14px; left: 50%; width: min(680px, calc(100% - 28px)); transform: translateX(-50%); display: grid; gap: 8px; pointer-events: none; }
article { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 13px 15px; border: 1px solid var(--line); border-left: 4px solid var(--primary); border-radius: 12px; background: var(--surface-glass-strong); box-shadow: var(--shadow-md); backdrop-filter: blur(12px); pointer-events: auto; }
.announcement-success { border-left-color: var(--success); }.announcement-warning { border-left-color: var(--warning); }.announcement-critical { border-left-color: var(--danger); }
strong { display: block; margin-bottom: 3px; } p { margin: 0; color: var(--muted); white-space: pre-wrap; line-height: 1.5; }
button { border: 0; background: transparent; color: var(--muted); font-size: 24px; cursor: pointer; }
</style>
