<template>
  <section class="standard-page account-announcements">
    <PageHeader title="公告中心" description="查看当前与历史站点公告；阅读状态会同步到你的账户。" eyebrow="NOTICES">
      <template #actions><PageRefreshButton :loading="loading" label="刷新公告" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :error="error" />
    <UiMetricStrip :items="[{ label: '未读公告', value: unreadCount }, { label: '公告总数', value: total }]" />
    <UiSection>
      <div v-if="items.length" class="announcement-list">
        <article v-for="item in items" :key="`${item.id}:${item.revision}`" :class="['announcement-card', { unread: !item.read }]">
          <button class="announcement-heading" type="button" :aria-expanded="openedId === item.id" @click="toggle(item)">
            <span :class="['severity-dot', `severity-${item.severity}`]"></span>
            <span class="heading-copy"><strong>{{ item.title }}</strong><small>{{ formatDate(item.created_at) }} · {{ stateLabel(item) }}</small></span>
            <StatusBadge :tone="item.read ? 'neutral' : 'info'">{{ item.read ? '已读' : '未读' }}</StatusBadge>
            <span class="chevron">{{ openedId === item.id ? '−' : '+' }}</span>
          </button>
          <div v-if="openedId === item.id" class="announcement-body">
            <MarkdownContent :content="item.content" />
            <footer><span>公告版本 {{ item.revision }}</span><span v-if="item.read_at">阅读于 {{ formatDate(item.read_at) }}</span></footer>
          </div>
        </article>
      </div>
      <EmptyState v-else-if="!loading" icon="info" title="暂无公告" description="已经发布的站点公告会保留在这里。" />
      <div class="pager" v-if="total > limit"><UiButton variant="ghost" type="button" :disabled="offset === 0 || loading" @click="previous">上一页</UiButton><span>{{ offset + 1 }}–{{ Math.min(offset + limit, total) }} / {{ total }}</span><UiButton variant="ghost" type="button" :disabled="offset + limit >= total || loading" @click="next">下一页</UiButton></div>
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchAccountAnnouncements, type AccountAnnouncement } from '../../api/client'
import EmptyState from '../../components/EmptyState.vue'
import MarkdownContent from '../../components/MarkdownContent.vue'
import PageHeader from '../../components/PageHeader.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import { useAppStore } from '../../stores/app'
import { normalizeApiErrorMessage } from '../../utils/apiError'

const app = useAppStore()
const loading = ref(false)
const error = ref('')
const items = ref<AccountAnnouncement[]>([])
const total = ref(0)
const unreadCount = ref(0)
const offset = ref(0)
const limit = 20
const openedId = ref(0)

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
  } finally {
    loading.value = false
  }
}
async function toggle(item: AccountAnnouncement) {
  openedId.value = openedId.value === item.id ? 0 : item.id
  if (!openedId.value || item.read) return
  try {
    await app.markAnnouncementRead(item)
    item.read = true
    item.read_at = new Date().toISOString()
    unreadCount.value = Math.max(0, unreadCount.value - 1)
  } catch (cause) {
    error.value = normalizeApiErrorMessage(cause, '已读状态同步失败，请刷新后重试。')
  }
}
function previous() { offset.value = Math.max(0, offset.value - limit); void load() }
function next() { offset.value += limit; void load() }
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function stateLabel(item: AccountAnnouncement) {
  if (item.status === 'archived') return '已归档'
  if (item.ends_at && new Date(item.ends_at).getTime() <= Date.now()) return '已结束'
  return '生效中'
}
onMounted(load)
</script>

<style scoped>
.announcement-list { display: grid; gap: 10px; }.announcement-card { overflow: hidden; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }.announcement-card.unread { border-color: var(--primary); box-shadow: inset 3px 0 0 var(--primary); }
.announcement-heading { width: 100%; display: grid; grid-template-columns: 9px minmax(0, 1fr) auto 20px; align-items: center; gap: 12px; padding: 15px 16px; border: 0; background: transparent; color: inherit; cursor: pointer; text-align: left; }.heading-copy { min-width: 0; display: grid; gap: 4px; }.heading-copy strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.heading-copy small, .announcement-body footer, .pager { color: var(--muted); font-size: 10px; }
.severity-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--primary); }.severity-success { background: var(--success); }.severity-warning { background: var(--warning); }.severity-critical { background: var(--danger); }.chevron { color: var(--muted); font-size: 18px; }
.announcement-body { padding: 4px 38px 18px; border-top: 1px solid var(--line); }.announcement-body :deep(.markdown-content) { padding-top: 15px; }.announcement-body footer { display: flex; justify-content: space-between; gap: 12px; margin-top: 18px; padding-top: 10px; border-top: 1px solid var(--line); }
.pager { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 18px; }
@media (max-width: 640px) { .announcement-heading { grid-template-columns: 9px minmax(0, 1fr) auto; }.chevron { display: none; }.announcement-body { padding-inline: 18px; } }
</style>
