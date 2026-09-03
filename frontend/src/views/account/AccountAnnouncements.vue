<template>
  <section class="standard-page account-announcements">
    <PageHeader title="公告中心" description="重要公告优先展开，历史通知保持轻量列表；未读只统计当前有效公告。" eyebrow="NOTICE BOARD">
      <template #actions><PageRefreshButton :loading="loading" label="刷新公告" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :error="error" />
    <UiMetricStrip :items="[{ label: '当前未读', value: unreadCount }, { label: '公告记录', value: total }, { label: '重点展示', value: featured.length }]" />

    <section v-if="featured.length" :class="['featured-board', `featured-count-${featured.length}`]" aria-label="重点公告">
      <article v-for="(item, index) in featured" :key="`featured:${item.id}:${item.revision}`" :class="['featured-card', `severity-${item.severity}`]">
        <header><span class="rank">0{{ index + 1 }}</span><span class="feature-state">{{ item.popup_enabled ? '重点提醒' : '当前公告' }}</span><StatusBadge :tone="isUnread(item) ? 'info' : 'neutral'">{{ isUnread(item) ? '未读' : '已读' }}</StatusBadge></header>
        <h2>{{ item.title }}</h2>
        <MarkdownContent :content="item.content" />
        <footer><span>{{ formatDate(item.created_at) }}</span><UiButton v-if="isUnread(item)" variant="ghost" size="sm" type="button" @click="markRead(item)">标记已读</UiButton><span v-else-if="item.read_at">阅读于 {{ formatDate(item.read_at) }}</span></footer>
      </article>
    </section>

    <UiSection title="全部通知" description="有效公告优先，其后按发布时间展示历史。">
      <div v-if="listItems.length" class="announcement-list">
        <article v-for="item in listItems" :key="`${item.id}:${item.revision}`" :class="['announcement-row', { unread: isUnread(item) }]">
          <button class="announcement-heading" type="button" :aria-expanded="openedId === item.id" @click="toggle(item)">
            <span :class="['severity-dot', `severity-${item.severity}`]"></span>
            <span class="heading-copy"><strong>{{ item.title }}</strong><small>{{ formatDate(item.created_at) }} · {{ stateLabel(item) }}</small></span>
            <StatusBadge :tone="badgeTone(item)">{{ badgeLabel(item) }}</StatusBadge>
            <span class="chevron">{{ openedId === item.id ? '−' : '+' }}</span>
          </button>
          <div v-if="openedId === item.id" class="announcement-body"><MarkdownContent :content="item.content" /><footer><span>公告版本 {{ item.revision }}</span><span v-if="item.read_at">阅读于 {{ formatDate(item.read_at) }}</span></footer></div>
        </article>
      </div>
      <EmptyState v-else-if="!loading && !featured.length" icon="info" title="暂无公告" description="已经发布的站点公告会保留在这里。" />
      <div class="pager" v-if="total > limit"><UiButton variant="ghost" type="button" :disabled="offset === 0 || loading" @click="previous">上一页</UiButton><span>{{ offset + 1 }}–{{ Math.min(offset + limit, total) }} / {{ total }}</span><UiButton variant="ghost" type="button" :disabled="offset + limit >= total || loading" @click="next">下一页</UiButton></div>
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchAccountAnnouncements, type AccountAnnouncement } from '../../api/client'
import EmptyState from '../../components/EmptyState.vue'
import MarkdownContent from '../../components/MarkdownContent.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import { useAppStore } from '../../stores/app'
import { normalizeApiErrorMessage } from '../../utils/apiError'

const app = useAppStore()
const loading = ref(false), error = ref('')
const items = ref<AccountAnnouncement[]>([])
const total = ref(0), unreadCount = ref(0), offset = ref(0), openedId = ref(0)
const limit = 20
const featured = computed(() => offset.value === 0 ? items.value.filter(item => item.active).slice(0, 3) : [])
const featuredIDs = computed(() => new Set(featured.value.map(item => item.id)))
const listItems = computed(() => items.value.filter(item => !featuredIDs.value.has(item.id)))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const page = await fetchAccountAnnouncements(offset.value, limit)
    items.value = page.items
    total.value = page.total
    unreadCount.value = page.unread_count
    app.announcementUnreadCount = page.unread_count
  } catch (cause) {
    error.value = normalizeApiErrorMessage(cause, '公告历史加载失败。')
  } finally { loading.value = false }
}
function isUnread(item: AccountAnnouncement) { return item.active && !item.read }
async function markRead(item: AccountAnnouncement) {
  if (!isUnread(item)) return
  try {
    await app.markAnnouncementRead(item)
    item.read = true
    item.read_at = new Date().toISOString()
    unreadCount.value = Math.max(0, unreadCount.value - 1)
  } catch (cause) { error.value = normalizeApiErrorMessage(cause, '已读状态同步失败，请刷新后重试。') }
}
async function toggle(item: AccountAnnouncement) { openedId.value = openedId.value === item.id ? 0 : item.id; if (openedId.value) await markRead(item) }
function previous() { offset.value = Math.max(0, offset.value - limit); void load() }
function next() { offset.value += limit; void load() }
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function stateLabel(item: AccountAnnouncement) { return item.active ? '生效中' : '已结束' }
function badgeLabel(item: AccountAnnouncement) { return !item.active ? '历史' : item.read ? '已读' : '未读' }
function badgeTone(item: AccountAnnouncement) { return !item.active || item.read ? 'neutral' : 'info' }
onMounted(load)
</script>

<style scoped>
.featured-board { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; }.featured-board.featured-count-1 { grid-template-columns: minmax(0, 1fr); }.featured-board.featured-count-2 { grid-template-columns: repeat(2, minmax(0, 1fr)); }.featured-card { min-width: 0; padding: 20px; border: 1px solid var(--line); border-top: 4px solid var(--primary); border-radius: 16px; background: var(--surface); box-shadow: 0 10px 34px var(--floating-shadow); }.featured-card.severity-critical { border-top-color: var(--danger); }.featured-card.severity-warning { border-top-color: var(--warning); }.featured-card.severity-success { border-top-color: var(--success); }.featured-card > header { display: flex; align-items: center; gap: 8px; }.rank { margin-right: auto; color: var(--line-strong); font-size: 24px; font-weight: 900; }.feature-state { color: var(--muted); font-size: 9px; font-weight: 800; letter-spacing: .08em; }.featured-card h2 { margin: 16px 0 12px; font-size: 18px; }.featured-card :deep(.markdown-content) { max-height: 260px; overflow: auto; font-size: 11px; }.featured-card > footer { display: flex; align-items: center; justify-content: space-between; min-height: 30px; margin-top: 16px; padding-top: 10px; border-top: 1px solid var(--line); color: var(--muted); font-size: 9px; }
.announcement-list { display: grid; gap: 8px; }.announcement-row { overflow: hidden; border: 1px solid var(--line); border-radius: 11px; background: var(--surface); }.announcement-row.unread { border-color: var(--primary); box-shadow: inset 3px 0 0 var(--primary); }.announcement-heading { width: 100%; display: grid; grid-template-columns: 9px minmax(0, 1fr) auto 20px; align-items: center; gap: 12px; padding: 13px 15px; border: 0; background: transparent; color: inherit; cursor: pointer; text-align: left; }.heading-copy { min-width: 0; display: grid; gap: 4px; }.heading-copy strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.heading-copy small, .announcement-body footer, .pager { color: var(--muted); font-size: 10px; }.severity-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--primary); }.severity-dot.severity-success { background: var(--success); }.severity-dot.severity-warning { background: var(--warning); }.severity-dot.severity-critical { background: var(--danger); }.chevron { color: var(--muted); font-size: 18px; }.announcement-body { padding: 4px 36px 16px; border-top: 1px solid var(--line); }.announcement-body :deep(.markdown-content) { padding-top: 14px; }.announcement-body footer { display: flex; justify-content: space-between; gap: 12px; margin-top: 16px; padding-top: 9px; border-top: 1px solid var(--line); }.pager { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 18px; }
@media (max-width: 920px) { .featured-board { grid-template-columns: 1fr; }.featured-card :deep(.markdown-content) { max-height: none; } } @media (max-width: 640px) { .announcement-heading { grid-template-columns: 9px minmax(0, 1fr) auto; }.chevron { display: none; }.announcement-body { padding-inline: 18px; } }
</style>
