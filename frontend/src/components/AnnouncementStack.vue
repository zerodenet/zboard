<template>
  <aside v-if="current" :class="['announcement-bar', `announcement-${current.severity}`]" aria-live="polite">
    <div class="announcement-summary">
      <span class="announcement-label">{{ severityLabel(current.severity) }}</span>
      <strong>{{ current.title }}</strong>
      <span class="announcement-excerpt">{{ excerpt(current.content) }}</span>
      <span v-if="visible.length > 1" class="announcement-count">另有 {{ visible.length - 1 }} 条未读</span>
    </div>
    <div class="announcement-actions">
      <RouterLink v-if="app.isAuthenticated" to="/account/announcements">查看公告中心</RouterLink>
      <button v-else type="button" @click="expanded = !expanded">{{ expanded ? '收起' : '查看详情' }}</button>
      <button type="button" class="acknowledge" :disabled="busy" @click="acknowledge(current)">
        {{ current.dismissible ? '关闭' : '我知道了' }}
      </button>
    </div>
    <MarkdownContent v-if="expanded && !app.isAuthenticated" class="announcement-detail" :content="current.content" />
  </aside>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Announcement } from '../api/client'
import { useAppStore } from '../stores/app'
import MarkdownContent from './MarkdownContent.vue'

const props = defineProps<{ items: Announcement[] }>()
const app = useAppStore()
const busy = ref(false)
const expanded = ref(false)
const hidden = ref<Record<string, boolean>>(readGuestReceipts())
const keyOf = (item: Announcement) => `${item.id}:${item.revision}`
const visible = computed(() => props.items.filter(item => !item.read && !hidden.value[keyOf(item)]))
const current = computed(() => visible.value[0])

function readGuestReceipts() {
  if (typeof localStorage === 'undefined') return {}
  try { return JSON.parse(localStorage.getItem('zboard.guestAnnouncementReceipts') || '{}') || {} } catch { return {} }
}
function excerpt(value: string) {
  return value.replace(/[#>*_`\[\]()\-]+/g, ' ').replace(/\s+/g, ' ').trim().slice(0, 120)
}
function severityLabel(value: Announcement['severity']) {
  return ({ info: '公告', success: '进展', warning: '提醒', critical: '重要' })[value]
}
async function acknowledge(item: Announcement) {
  if (busy.value) return
  busy.value = true
  try {
    if (app.isAuthenticated) {
      await app.markAnnouncementRead(item)
    } else {
      hidden.value = { ...hidden.value, [keyOf(item)]: true }
      localStorage.setItem('zboard.guestAnnouncementReceipts', JSON.stringify(hidden.value))
    }
    expanded.value = false
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.announcement-bar { position: relative; z-index: 20; display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 10px 18px; padding: 10px clamp(16px, 4vw, 34px); border-bottom: 1px solid var(--line); border-left: 4px solid var(--primary); background: var(--surface); box-shadow: 0 4px 16px var(--floating-shadow); }
.announcement-warning { border-left-color: var(--warning); }.announcement-critical { border-left-color: var(--danger); }.announcement-success { border-left-color: var(--success); }
.announcement-summary, .announcement-actions { display: flex; align-items: center; gap: 10px; min-width: 0; }
.announcement-label, .announcement-count { flex: none; padding: 3px 7px; border-radius: 999px; background: var(--surface-soft); color: var(--muted); font-size: 10px; font-weight: 750; }
.announcement-summary strong { flex: none; font-size: 12px; }.announcement-excerpt { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--muted); font-size: 11px; }
.announcement-actions { justify-content: flex-end; }.announcement-actions a, .announcement-actions button { border: 0; background: transparent; color: var(--primary); cursor: pointer; font: inherit; font-size: 11px; font-weight: 700; text-decoration: none; }
.announcement-actions .acknowledge { padding: 5px 9px; border: 1px solid var(--line-strong); border-radius: 7px; color: var(--text); }.announcement-actions button:disabled { cursor: wait; opacity: .6; }
.announcement-detail { grid-column: 1 / -1; max-width: 860px; padding: 12px 0 4px; }
@media (max-width: 760px) { .announcement-bar { grid-template-columns: 1fr; }.announcement-excerpt { display: none; }.announcement-actions { justify-content: flex-start; } }
</style>
