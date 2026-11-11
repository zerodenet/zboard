<template>
  <section class="standard-page account-announcements">
    <PageHeader title="公告">
      <template #actions><span v-if="loaded" class="announcement-count">{{ unreadCount ? `${unreadCount} 条未读 · ` : '' }}{{ total }} 条</span><PageRefreshButton :loading="loading" label="刷新公告" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :error="error" />

    <section v-if="featured.length" :class="['featured-board', `featured-count-${featured.length}`]" aria-label="重点公告">
      <article v-for="item in featured" :key="`featured:${item.id}:${item.revision}`" :class="['featured-card', `severity-${item.severity}`]">
        <h2><button class="featured-heading" type="button" :aria-expanded="openedId === item.id" @click="toggle(item)"><span><strong>{{ item.title }}</strong><time :datetime="item.created_at">{{ formatDate(item.created_at) }}</time></span><StatusBadge v-if="isUnread(item)" tone="info">未读</StatusBadge><span class="chevron" aria-hidden="true">{{ openedId === item.id ? '−' : '+' }}</span></button></h2>
        <div v-if="openedId === item.id" class="featured-body"><MarkdownContent :content="item.content" /></div>
      </article>
    </section>

    <UiSection v-if="listItems.length || !featured.length || total > limit" :title="featured.length ? '历史公告' : ''">
      <TableSkeleton v-if="loading && !loaded" label="正在加载公告" :columns="1" :rows="3" />
      <div v-if="listItems.length" class="announcement-list">
        <article v-for="item in listItems" :key="`${item.id}:${item.revision}`" :class="['announcement-row', { unread: isUnread(item) }]">
          <button class="announcement-heading" type="button" :aria-expanded="openedId === item.id" @click="toggle(item)">
            <span class="heading-copy"><strong>{{ item.title }}</strong><small>{{ formatDate(item.created_at) }}</small></span>
            <StatusBadge v-if="isUnread(item)" tone="info">未读</StatusBadge>
            <span class="chevron">{{ openedId === item.id ? '−' : '+' }}</span>
          </button>
          <div v-if="openedId === item.id" class="announcement-body"><MarkdownContent :content="item.content" /></div>
        </article>
      </div>
      <EmptyState v-else-if="loaded && !error && !featured.length" icon="info" title="暂无公告" description="" />
      <TablePager v-if="total > limit" :total="total" :offset="offset" :limit="limit" :loading="loading" :page-sizes="[20, 50, 100]" @change="changePage" />
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchAccountAnnouncements, type AccountAnnouncement } from '../../api/client'
import TableSkeleton from '../../components/TableSkeleton.vue'
import EmptyState from '../../components/EmptyState.vue'
import MarkdownContent from '../../components/MarkdownContent.vue'
import PageHeader from '../../components/PageHeader.vue'
import TablePager from '../../components/TablePager.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import { useAppStore } from '../../stores/app'
import { normalizeApiErrorMessage } from '../../utils/apiError'

const app = useAppStore()
const loading = ref(false), loaded = ref(false), error = ref('')
const items = ref<AccountAnnouncement[]>([])
const total = ref(0), unreadCount = ref(0), offset = ref(0), openedId = ref(0)
const limit = ref(20)
const featured = computed(() => offset.value === 0 ? items.value.filter(item => item.active).slice(0, 3) : [])
const featuredIDs = computed(() => new Set(featured.value.map(item => item.id)))
const listItems = computed(() => items.value.filter(item => !featuredIDs.value.has(item.id)))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const page = await fetchAccountAnnouncements(offset.value, limit.value)
    loaded.value = true
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
function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; void load() }
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
onMounted(load)
</script>

<style scoped>
.announcement-count { color: var(--muted-foreground); font-size: 12px; }
.featured-board, .announcement-list { display: grid; gap: 12px; }
.featured-card { min-width: 0; padding: 20px; border: 0; border-radius: var(--radius-md); background: var(--surface-soft); }
.featured-card.severity-critical { box-shadow: inset 3px 0 var(--danger); }
.featured-card.severity-warning { box-shadow: inset 3px 0 var(--warning); }
.featured-card h2 { margin: 0; font-size: 15px; overflow-wrap: anywhere; }
.featured-heading, .announcement-heading { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 48px; padding: 0; border: 0; color: inherit; background: transparent; text-align: left; cursor: pointer; }
.featured-heading > span:first-child, .heading-copy { flex: 1; min-width: 0; display: grid; gap: 6px; }
.featured-heading strong, .heading-copy strong { font-size: 14px; font-weight: 600; overflow-wrap: anywhere; }
.featured-heading time, .heading-copy small { font-size: 12px; font-weight: 400; color: var(--muted-foreground); }
.featured-heading:focus-visible, .announcement-heading:focus-visible { outline: 2px solid var(--ring); outline-offset: 4px; }
.announcement-row { min-width: 0; padding: 12px 0; border: 0; background: transparent; }
.chevron { color: var(--muted-foreground); font-size: 18px; }
.featured-body, .announcement-body { margin-top: 16px; min-width: 0; overflow-wrap: anywhere; }
.featured-body :deep(.markdown-content), .announcement-body :deep(.markdown-content) { font-size: 13px; }
</style>
