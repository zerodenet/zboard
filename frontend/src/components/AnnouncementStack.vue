<template>
  <div v-if="open && selected" class="announcement-backdrop" @click.self="closePrompt">
    <section :class="['announcement-dialog', { 'has-queue': related.length }]" role="dialog" aria-modal="true" aria-labelledby="announcement-dialog-title">
      <header class="dialog-header">
        <div><span class="eyebrow">NOTICE DESK</span><h2 id="announcement-dialog-title">最新公告</h2><p>当前仅加载最近 5 条有效公告。</p></div>
        <button type="button" class="close-button" aria-label="暂时关闭公告" @click="closePrompt">×</button>
      </header>
      <nav class="announcement-tabs" role="tablist" aria-label="公告分类">
        <button type="button" role="tab" :aria-selected="tab === 'priority'" :class="{ active: tab === 'priority' }" @click="selectTab('priority')">重点公告 <span>{{ priorityItems.length }}</span></button>
        <button type="button" role="tab" :aria-selected="tab === 'other'" :class="{ active: tab === 'other' }" :disabled="!otherItems.length" @click="selectTab('other')">其他未读 <span>{{ otherItems.length }}</span></button>
      </nav>
      <div :class="['announcement-layout', { 'has-queue': related.length }]">
        <article :class="['announcement-feature', `severity-${selected.severity}`]">
          <div class="feature-meta"><span>{{ severityLabel(selected.severity) }}</span><time>{{ formatDate(selected.updated_at) }}</time></div>
          <h3>{{ selected.title }}</h3>
          <MarkdownContent :content="selected.content" />
          <footer>
            <RouterLink v-if="app.isAuthenticated" to="/account/announcements" @click="closePrompt">进入公告中心</RouterLink>
            <span v-else>访客阅读记录仅保存在当前浏览器</span>
            <button type="button" class="acknowledge" :disabled="busy" @click="acknowledge(selected)">{{ selected.dismissible ? '关闭并标记已读' : '我知道了' }}</button>
          </footer>
        </article>
        <aside v-if="related.length" class="announcement-queue" aria-label="其他公告">
          <span class="queue-title">其他 {{ related.length }} 条</span>
          <button v-for="(item, index) in related" :key="keyOf(item)" type="button" @click="selectItem(item)">
            <span class="queue-rank">{{ String(index + 2).padStart(2, '0') }}</span>
            <span><strong>{{ item.title }}</strong><small>{{ severityLabel(item.severity) }} · {{ formatDate(item.updated_at) }}</small></span>
          </button>
        </aside>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Announcement } from '../api/client'
import { useAppStore } from '../stores/app'
import MarkdownContent from './MarkdownContent.vue'

const props = defineProps<{ items: Announcement[] }>()
const app = useAppStore()
const busy = ref(false)
const open = ref(false)
const tab = ref<'priority' | 'other'>('priority')
const selectedKey = ref('')
const lastPromptKey = ref('')
const hidden = ref<Record<string, boolean>>(readGuestReceipts())
const keyOf = (item: Announcement) => `${item.id}:${item.revision}`
const visible = computed(() => props.items.filter(item => !item.read && !hidden.value[keyOf(item)]).slice(0, 5))
const priorityItems = computed(() => visible.value.filter(item => item.popup_enabled))
const otherItems = computed(() => visible.value.filter(item => !item.popup_enabled))
const candidates = computed(() => tab.value === 'priority' ? priorityItems.value : otherItems.value)
const selected = computed(() => candidates.value.find(item => keyOf(item) === selectedKey.value) || candidates.value[0])
const related = computed(() => {
  const currentKey = selected.value ? keyOf(selected.value) : ''
  const source = tab.value === 'priority' ? visible.value : otherItems.value
  return source.filter(item => keyOf(item) !== currentKey).slice(0, 4)
})

watch(() => priorityItems.value.map(keyOf).join(','), value => {
  const first = priorityItems.value[0]
  if (!first || !value || keyOf(first) === lastPromptKey.value) return
  tab.value = 'priority'
  selectedKey.value = keyOf(first)
  lastPromptKey.value = keyOf(first)
  open.value = true
}, { immediate: true })

function readGuestReceipts() {
  if (typeof localStorage === 'undefined') return {}
  try { return JSON.parse(localStorage.getItem('zboard.guestAnnouncementReceipts') || '{}') || {} } catch { return {} }
}
function selectTab(value: 'priority' | 'other') {
  if (value === 'other' && !otherItems.value.length) return
  tab.value = value
  const first = (value === 'priority' ? priorityItems.value : otherItems.value)[0]
  selectedKey.value = first ? keyOf(first) : ''
}
function selectItem(item: Announcement) { tab.value = item.popup_enabled ? 'priority' : 'other'; selectedKey.value = keyOf(item) }
function closePrompt() { open.value = false }
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) }
function severityLabel(value: Announcement['severity']) { return ({ info: '公告', success: '进展', warning: '提醒', critical: '重要' })[value] }
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
    const remaining = candidates.value.filter(candidate => keyOf(candidate) !== keyOf(item))
    if (remaining.length) selectedKey.value = keyOf(remaining[0])
    else if (tab.value === 'priority' && otherItems.value.length) selectTab('other')
    else open.value = false
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.announcement-backdrop { position: fixed; z-index: 1350; inset: 0; display: grid; place-items: center; padding: 20px; background: color-mix(in srgb, var(--navigation-scrim) 88%, transparent); backdrop-filter: blur(8px); }
.announcement-dialog { width: min(720px, 100%); max-height: min(760px, calc(100vh - 40px)); overflow: auto; border: 1px solid var(--line-strong); border-radius: 24px; background: var(--surface); box-shadow: 0 32px 100px var(--sidebar-shadow); }
.announcement-dialog.has-queue { width: min(960px, 100%); }
.dialog-header { display: flex; justify-content: space-between; gap: 20px; padding: 24px 26px 18px; }.dialog-header h2 { margin: 4px 0; font-size: 24px; }.dialog-header p { margin: 0; color: var(--muted); font-size: 11px; }.eyebrow { color: var(--primary); font-size: 9px; font-weight: 850; letter-spacing: .16em; }.close-button { width: 34px; height: 34px; border: 1px solid var(--line); border-radius: 50%; background: transparent; color: var(--muted); cursor: pointer; font-size: 22px; }
.announcement-tabs { display: flex; gap: 6px; padding: 0 26px; border-bottom: 1px solid var(--line); }.announcement-tabs button { position: relative; padding: 10px 13px 12px; border: 0; background: transparent; color: var(--muted); cursor: pointer; font: inherit; font-size: 11px; font-weight: 750; }.announcement-tabs button.active { color: var(--text); }.announcement-tabs button.active::after { position: absolute; right: 8px; bottom: -1px; left: 8px; height: 2px; background: var(--primary); content: ''; }.announcement-tabs button span { margin-left: 5px; color: var(--primary); }.announcement-tabs button:disabled { cursor: default; opacity: .45; }
.announcement-layout { display: grid; grid-template-columns: minmax(0, 1fr); min-height: 390px; }.announcement-layout.has-queue { grid-template-columns: minmax(0, 1fr) 270px; }.announcement-feature { padding: 28px 30px; border-top: 4px solid var(--primary); }.announcement-feature.severity-critical { border-color: var(--danger); }.announcement-feature.severity-warning { border-color: var(--warning); }.announcement-feature.severity-success { border-color: var(--success); }.feature-meta { display: flex; justify-content: space-between; gap: 12px; color: var(--muted); font-size: 10px; }.feature-meta span { color: var(--primary); font-weight: 800; }.announcement-feature h3 { margin: 14px 0 20px; font-size: clamp(22px, 4vw, 34px); line-height: 1.2; }.announcement-feature footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 28px; padding-top: 16px; border-top: 1px solid var(--line); color: var(--muted); font-size: 10px; }.announcement-feature footer a { color: var(--primary); font-weight: 750; text-decoration: none; }.acknowledge { padding: 9px 13px; border: 0; border-radius: 9px; background: var(--primary); color: var(--text-inverse); cursor: pointer; font: inherit; font-size: 11px; font-weight: 800; }
.announcement-queue { display: grid; align-content: start; gap: 6px; padding: 20px 18px; border-left: 1px solid var(--line); background: var(--surface-soft); }.queue-title { padding: 0 8px 8px; color: var(--muted); font-size: 9px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; }.announcement-queue button { display: grid; grid-template-columns: 28px minmax(0, 1fr); gap: 8px; padding: 11px 8px; border: 0; border-radius: 10px; background: transparent; color: inherit; cursor: pointer; text-align: left; }.announcement-queue button:hover { background: var(--surface); }.queue-rank { color: var(--line-strong); font-size: 16px; font-weight: 900; }.announcement-queue button span:last-child { display: grid; gap: 4px; min-width: 0; }.announcement-queue strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }.announcement-queue small { color: var(--muted); font-size: 9px; }
@media (max-width: 720px) { .announcement-layout { grid-template-columns: 1fr; }.announcement-queue { border-top: 1px solid var(--line); border-left: 0; }.announcement-feature { padding: 22px 20px; }.announcement-feature footer { align-items: flex-start; flex-direction: column; }.acknowledge { width: 100%; }.dialog-header, .announcement-tabs { padding-inline: 20px; } }
</style>
