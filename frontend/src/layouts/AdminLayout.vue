<template>
  <div class="app-shell admin-shell" :class="{ 'nav-open': navigationOpen }">
    <button v-if="drawerModal" type="button" class="nav-scrim" aria-label="关闭导航遮罩" tabindex="-1" @click="navigationOpen = false" />
    <aside id="admin-navigation" ref="sidebar" class="app-sidebar admin-sidebar"
      :inert="mobile && !navigationOpen" :role="drawerModal ? 'dialog' : undefined"
      :aria-modal="drawerModal ? true : undefined" aria-label="管理导航">
      <div class="brand-block">
        <RouterLink class="admin-brand-home" to="/admin/dashboard" @click="navigationOpen = false">
          <span class="brand-mark">Z</span><div><strong>{{ app.siteName }}</strong><span>管理后台</span></div>
        </RouterLink>
        <button class="sidebar-close nav-icon-button" type="button" aria-label="关闭导航" @click="navigationOpen = false"><UiIcon name="close" /></button>
      </div>

      <AdminNavigation @select-page="navigationOpen = false" />

      <AdminVersionStatus :info="systemInfo" />
    </aside>

    <div class="app-workspace" :inert="drawerModal">
      <header class="topbar">
        <div class="topbar-leading">
          <button class="nav-icon-button menu-button" type="button" :aria-label="navigationOpen ? '关闭导航' : '打开导航'" :aria-expanded="navigationOpen" aria-controls="admin-navigation" @click="navigationOpen = !navigationOpen"><UiIcon :name="navigationOpen ? 'close' : 'menu'" /></button>
          <AdminQuickSearch />
          <nav class="topbar-breadcrumb" aria-label="当前位置"><span class="topbar-context">{{ currentSection }}</span><span class="topbar-breadcrumb-separator" aria-hidden="true">/</span><strong>{{ currentTitle }}</strong></nav>
        </div>
        <DropdownMenuRoot>
          <DropdownMenuTrigger as-child>
            <button type="button" class="topbar-account-trigger" aria-label="账户菜单" :title="app.user.email || '账户菜单'">
              <span class="topbar-account-avatar" aria-hidden="true">{{ userInitial }}</span>
              <span class="topbar-account-name">{{ app.user.email || '管理员' }}</span>
              <UiIcon name="chevron" class="topbar-account-chevron" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuPortal>
            <DropdownMenuContent class="ui-dropdown-content topbar-account-menu" align="end" :side-offset="8">
              <div class="topbar-account-menu-heading">{{ app.user.email || '管理员' }}</div>
              <DropdownMenuSeparator class="topbar-account-menu-separator" />
              <DropdownMenuItem as-child class="ui-dropdown-item"><RouterLink to="/"><UiIcon name="dashboard" />查看站点</RouterLink></DropdownMenuItem>
              <DropdownMenuItem as-child class="ui-dropdown-item"><RouterLink to="/account"><UiIcon name="users" />个人中心</RouterLink></DropdownMenuItem>
              <DropdownMenuSeparator class="topbar-account-menu-separator" />
              <DropdownMenuItem class="ui-dropdown-item" @select="logout"><UiIcon name="logout" />退出登录</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuPortal>
        </DropdownMenuRoot>
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
import { computed, onMounted, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuSeparator, DropdownMenuTrigger } from 'reka-ui'
import AdminNavigation from '../components/AdminNavigation.vue'
import AdminQuickSearch from '../components/AdminQuickSearch.vue'
import AdminVersionStatus from '../components/AdminVersionStatus.vue'
import TaskTray from '../components/TaskTray.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAdminDrawer } from '../composables/useAdminDrawer'
import { useAppStore } from '../stores/app'
import { resolveAdminNavigation } from '../utils/adminNavigation'
import { normalizeAdminReturnTo } from '../utils/navigation'
import { fetchAdminSystemInfo, type AdminSystemInfo } from '../api/system'

const app = useAppStore()
provide('admin-page-navigation', true)
const route = useRoute()
const router = useRouter()
const sidebar = ref<HTMLElement | null>(null)
const { open: navigationOpen, mobile, modal: drawerModal } = useAdminDrawer(sidebar)
const currentTitle = computed(() => String(route.meta.title || '管理后台'))
const currentSection = computed(() => resolveAdminNavigation(route.path)?.domain.label || '管理控制台')
const returnTarget = computed(() => normalizeAdminReturnTo(route.query.return_to))
const userInitial = computed(() => (app.user.email || 'Z').slice(0, 1).toUpperCase())
const systemInfo = ref<AdminSystemInfo | null>(null)
onMounted(() => {
  void app.loadMe()
  void fetchAdminSystemInfo().then(info => { systemInfo.value = info }).catch(() => { /* Version metadata is optional in local previews. */ })
})
watch(() => route.fullPath, () => { navigationOpen.value = false })
function logout() { app.clear(); router.push('/') }
</script>

<style scoped>
.admin-shell { grid-template-columns: 248px minmax(0, 1fr); background: var(--background); }
.admin-sidebar { height: 100dvh; color: var(--foreground); background: var(--surface-subtle); border-right-color: var(--border); }
.admin-sidebar .brand-block { min-height: 64px; padding: 11px 16px; border-color: var(--border); }
.admin-brand-home { min-width: 0; display: flex; align-items: center; gap: 10px; text-decoration: none; }
.admin-brand-home .brand-mark { width: 30px; height: 30px; border-radius: 7px; color: var(--foreground); background: var(--card); border: 1px solid var(--border); font-size: 15px; }
.admin-brand-home strong { color: var(--foreground); font-size: 13px; font-weight: 600; }
.admin-brand-home span:not(.brand-mark) { color: var(--muted-foreground); font-size: 10px; }
.nav-icon-button { flex: 0 0 auto; display: inline-grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 0; border-radius: 6px; color: var(--muted-foreground); background: transparent; cursor: pointer; font-size: 16px; }
.nav-icon-button:hover { background: var(--muted-surface); color: var(--foreground); }
.nav-icon-button:focus-visible, .admin-brand-home:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.admin-task-layer { display: contents; }
.admin-shell .topbar { min-height: 64px; padding: 0 40px; background: var(--card); border-bottom-color: var(--border); }
.admin-shell .topbar-leading { min-width: 0; gap: 18px; }
.topbar-breadcrumb { display: flex; align-items: center; gap: 9px; min-width: 0; }
.topbar-breadcrumb strong { overflow: hidden; color: var(--foreground); font-size: 12px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.topbar-context { color: var(--muted-foreground); font-size: 12px; white-space: nowrap; }
.topbar-context::after { content: none; }
.topbar-breadcrumb-separator { color: var(--input); font-size: 13px; }
.topbar-account-trigger { min-width: 36px; max-width: 210px; height: 38px; display: inline-flex; align-items: center; justify-content: flex-end; gap: 9px; margin-left: auto; padding: 3px 6px 3px 3px; border: 0; border-radius: 8px; color: var(--text-secondary); background: transparent; cursor: pointer; }
.topbar-account-trigger:hover, .topbar-account-trigger[data-state='open'] { color: var(--foreground); background: var(--surface-subtle); }
.topbar-account-trigger:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.topbar-account-avatar { width: 30px; height: 30px; display: grid; place-items: center; flex: none; border: 1px solid var(--border); border-radius: 50%; color: var(--foreground); background: var(--surface-subtle); font-size: 12px; font-weight: 600; }
.topbar-account-name { min-width: 0; overflow: hidden; font-size: 12px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.topbar-account-chevron { width: 13px; transform: rotate(90deg); transition: transform .16s ease-out; }
.topbar-account-trigger[data-state='open'] .topbar-account-chevron { transform: rotate(-90deg); }
.topbar-account-menu { min-width: 200px; max-width: min(280px, calc(100vw - 24px)); }
.topbar-account-menu-heading { overflow: hidden; padding: 8px 10px 9px; color: var(--muted-foreground); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.topbar-account-menu-separator { height: 1px; margin: 4px 0; background: var(--border); }
.topbar-account-menu .ui-dropdown-item { display: flex; align-items: center; gap: 10px; }
.topbar-account-menu .ui-dropdown-item .ui-icon { width: 15px; height: 15px; color: var(--muted-foreground); }
.admin-shell .app-content { width: 100%; max-width: none; padding: 24px 40px 64px; }
.context-return-bar { min-height: 38px; display: flex; align-items: center; gap: 10px; padding: 6px 36px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); color: var(--muted-foreground); font-size: 11px; }
.context-return-bar a { display: inline-flex; align-items: center; gap: 5px; color: var(--foreground); font-size: 11px; font-weight: 500; text-decoration: none; }
.context-return-bar a:hover { text-decoration: underline; }
.context-return-bar .ui-icon { transform: rotate(180deg); }
@media (max-width: 1400px) and (min-width: 1101px) { .admin-shell .topbar { padding-inline: 32px; } .admin-shell .app-content { padding-inline: 32px; } }
@media (max-width: 1100px) and (min-width: 821px) { .admin-shell { grid-template-columns: 224px minmax(0, 1fr); } .admin-shell .topbar { padding-inline: 24px; } .admin-shell .app-content { padding-inline: 24px; } }
@media (max-width: 1100px) { .topbar-account-name { display: none; } .topbar-account-trigger { padding-inline: 3px; } }
@media (max-width: 820px) {
  .admin-sidebar { width: min(320px, 92vw); }
  /* The global maintenance badge must not cover the drawer's account actions. */
  :global(.admin-shell.nav-open ~ .admin-maintenance-banner) { visibility: hidden; }
  .admin-shell .topbar { padding-inline: 16px; }
  .admin-shell .app-content { padding: 20px 18px 48px; }
  .context-return-bar { padding-inline: 18px; }
  .context-return-bar > span { display: none; }
}
@media (max-width: 560px) { .admin-shell .app-content { padding-inline: 14px; } }
@media (prefers-reduced-motion: reduce) { .admin-sidebar, .topbar-account-chevron { transition: none; } }
</style>
