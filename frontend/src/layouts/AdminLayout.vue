<template>
  <div class="app-shell admin-shell" :class="{ 'nav-open': navigationOpen }">
    <button v-if="drawerModal" type="button" class="nav-scrim" aria-label="关闭导航遮罩" tabindex="-1" @click="navigationOpen = false" />
    <aside id="admin-navigation" ref="sidebar" class="app-sidebar admin-sidebar"
      :inert="mobile && !navigationOpen" :role="drawerModal ? 'dialog' : undefined"
      :aria-modal="drawerModal ? true : undefined" aria-label="管理导航">
      <div class="brand-block">
        <RouterLink class="admin-brand-home" to="/admin/dashboard" @click="navigationOpen = false">
          <span class="brand-mark">Z</span><div><strong>{{ app.siteName }}</strong><span>管理控制台</span></div>
        </RouterLink>
        <button class="sidebar-close nav-icon-button" type="button" aria-label="关闭导航" @click="navigationOpen = false"><UiIcon name="close" /></button>
      </div>

      <AdminNavigation @select-domain="selectDomain" @select-page="navigationOpen = false" />

      <div class="admin-account">
        <span class="avatar">{{ userInitial }}</span>
        <div class="admin-account-identity"><strong :title="app.user.email">{{ app.user.email }}</strong><span>管理员</span></div>
        <button class="nav-icon-button" type="button" title="退出登录" aria-label="退出登录" @click="logout"><UiIcon name="logout" /></button>
      </div>
    </aside>

    <div class="app-workspace" :inert="drawerModal">
      <header class="topbar">
        <div class="topbar-leading">
          <button class="nav-icon-button menu-button" type="button" :aria-label="navigationOpen ? '关闭导航' : '打开导航'" :aria-expanded="navigationOpen" aria-controls="admin-navigation" @click="navigationOpen = !navigationOpen"><UiIcon :name="navigationOpen ? 'close' : 'menu'" /></button>
          <div><span class="topbar-context">{{ currentSection }}</span><strong>{{ currentTitle }}</strong></div>
        </div>
        <div class="topbar-status"><RouterLink class="topbar-link" to="/account">个人中心</RouterLink><span class="topbar-separator" /><RouterLink class="topbar-link muted-link" to="/">查看站点</RouterLink></div>
      </header>
      <nav v-if="returnTarget" class="context-return-bar" aria-label="跨资源返回">
        <RouterLink :to="returnTarget"><UiIcon name="chevron" />返回来源</RouterLink>
        <span>恢复上一个列表的筛选、页码和详情</span>
      </nav>
      <main class="app-content admin-stripe-surface"><RouterView /></main>
    </div>
    <div class="admin-task-layer" :inert="drawerModal"><TaskTray /></div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AdminNavigation from '../components/AdminNavigation.vue'
import TaskTray from '../components/TaskTray.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAdminDrawer } from '../composables/useAdminDrawer'
import { useAppStore } from '../stores/app'
import { resolveAdminNavigation } from '../utils/adminNavigation'
import { normalizeAdminReturnTo } from '../utils/navigation'

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const sidebar = ref<HTMLElement | null>(null)
const { open: navigationOpen, mobile, modal: drawerModal } = useAdminDrawer(sidebar)
const currentTitle = computed(() => String(route.meta.title || '管理后台'))
const currentSection = computed(() => resolveAdminNavigation(route.path)?.domain.label || '管理控制台')
const returnTarget = computed(() => normalizeAdminReturnTo(route.query.return_to))
const userInitial = computed(() => (app.user.email || 'Z').slice(0, 1).toUpperCase())
let selectingDomain = false

async function selectDomain(path: string) {
  // Keep the mobile drawer open so a domain and its page can be chosen together.
  selectingDomain = true
  try { await router.push(path) } finally { selectingDomain = false }
}

onMounted(() => { void app.loadMe() })
watch(() => route.fullPath, () => { if (!selectingDomain) navigationOpen.value = false })
function logout() { app.clear(); router.push('/') }
</script>

<style scoped>
.admin-shell { grid-template-columns: 280px minmax(0, 1fr); }
.admin-sidebar { height: 100dvh; color: var(--text-body); background: var(--surface); border-right-color: var(--line); }
.admin-sidebar .brand-block { min-height: 76px; padding: 16px 20px; border-color: var(--line-subtle); }
.admin-brand-home { min-width: 0; display: flex; align-items: center; gap: 12px; text-decoration: none; }
.admin-brand-home .brand-mark { width: 34px; height: 34px; border-radius: 10px; }
.admin-brand-home strong { color: var(--text-strong); font-size: 15px; }
.admin-brand-home span:not(.brand-mark) { color: var(--muted); font-size: 11px; }
.admin-account { display: flex; align-items: center; gap: 10px; min-height: 76px; padding: 14px 16px; border-top: 1px solid var(--line-subtle); }
.admin-account .avatar { width: 32px; height: 32px; flex: 0 0 auto; border-radius: 10px; font-size: 13px; }
.admin-account-identity { flex: 1; min-width: 0; display: grid; gap: 3px; }
.admin-account strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-body); font-size: 12px; font-weight: 600; }
.admin-account-identity > span { color: var(--muted); font-size: 11px; }
.nav-icon-button { flex: 0 0 auto; display: inline-grid; place-items: center; width: 34px; height: 34px; padding: 0; border: 0; border-radius: 8px; color: var(--text-secondary); background: transparent; cursor: pointer; font-size: 16px; }
.nav-icon-button:hover { background: var(--surface-muted); color: var(--text-strong); }
.nav-icon-button:focus-visible, .admin-brand-home:focus-visible { outline: 2px solid var(--focus-border); outline-offset: 2px; }
.admin-task-layer { display: contents; }
.topbar-link { color: var(--primary); font-weight: 650; text-decoration: none; }
.topbar-link.muted-link { color: var(--muted); }.topbar-separator { width: 1px; height: 13px; background: var(--line-strong); }
.context-return-bar { min-height: 38px; display: flex; align-items: center; gap: 10px; padding: 6px 32px; border-bottom: 1px solid var(--line); background: var(--primary-soft); color: var(--muted); font-size: 10px; }
.context-return-bar a { display: inline-flex; align-items: center; gap: 5px; color: var(--primary); font-size: 11px; font-weight: 750; text-decoration: none; }
.context-return-bar a:hover { text-decoration: underline; }
.context-return-bar .ui-icon { transform: rotate(180deg); }
@media (max-width: 820px) {
  .admin-sidebar { width: min(320px, 92vw); }
  /* The global maintenance badge must not cover the drawer's account actions. */
  :global(.admin-shell.nav-open ~ .admin-maintenance-banner) { visibility: hidden; }
  .context-return-bar { padding-inline: 18px; }
  .context-return-bar > span { display: none; }
}
@media (prefers-reduced-motion: reduce) { .admin-sidebar { transition: none; } }
</style>
