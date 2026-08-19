<template>
  <div class="app-shell" :class="{ 'nav-open': navigationOpen }">
    <UiButton v-if="navigationOpen" variant="ghost" class="nav-scrim" aria-label="关闭导航" @click="navigationOpen = false"></UiButton>
    <aside id="admin-navigation" class="app-sidebar">
      <div class="brand-block">
        <RouterLink class="admin-brand-home" to="/admin/dashboard"><span class="brand-mark">Z</span><div><strong>{{ app.siteName }}</strong><span>运营控制台</span></div></RouterLink>
        <UiButton variant="ghost" icon class="icon-button sidebar-close" type="button" aria-label="关闭导航" @click="navigationOpen = false"><UiIcon name="close" /></UiButton>
      </div>

      <nav class="primary-nav" aria-label="管理端主导航">
        <section v-for="group in navigationGroups" :key="group.label" class="nav-group">
          <p>{{ group.label }}</p>
          <template v-for="item in group.items" :key="item.to || item.label">
            <div v-if="item.children?.length" class="nav-subgroup" :class="{ active: item.children.some(child => isNavigationItemActive(child.to)) }">
              <div class="nav-subgroup-label"><UiIcon :name="item.icon" /><span>{{ item.label }}</span></div>
              <RouterLink v-for="child in item.children" :key="child.to" class="nav-child" :to="child.to" :class="{ 'router-link-active': isNavigationItemActive(child.to) }"><span>{{ child.label }}</span></RouterLink>
            </div>
            <RouterLink v-else-if="item.to" :to="item.to" :class="{ 'router-link-active': isNavigationItemActive(item.to) }"><UiIcon :name="item.icon" /><span>{{ item.label }}</span></RouterLink>
          </template>
        </section>
      </nav>

      <div class="sidebar-footer">
        <div class="account-summary">
          <span class="avatar">{{ userInitial }}</span>
          <div><strong>{{ app.user.email }}</strong><span>用户 · 管理员权限</span></div>
          <UiButton variant="ghost" icon class="icon-button" type="button" title="退出登录" aria-label="退出登录" @click="logout"><UiIcon name="logout" /></UiButton>
        </div>
        <p class="build-version" :title="fullVersion">{{ displayVersion }}</p>
      </div>
    </aside>

    <div class="app-workspace">
      <header class="topbar">
        <div class="topbar-leading">
          <UiButton variant="ghost" icon class="icon-button menu-button" type="button" :aria-label="navigationOpen ? '关闭导航' : '打开导航'" :aria-expanded="navigationOpen" aria-controls="admin-navigation" @click="navigationOpen = !navigationOpen"><UiIcon :name="navigationOpen ? 'close' : 'menu'" /></UiButton>
          <div><span class="topbar-context">{{ currentSection }}</span><strong>{{ currentTitle }}</strong></div>
        </div>
        <div class="topbar-status"><RouterLink class="topbar-link" to="/account">个人中心</RouterLink><span class="topbar-separator"></span><RouterLink class="topbar-link muted-link" to="/">查看站点</RouterLink></div>
      </header>
      <nav v-if="returnTarget" class="context-return-bar" aria-label="跨资源返回">
        <RouterLink :to="returnTarget"><UiIcon name="chevron" />返回来源</RouterLink>
        <span>恢复上一个列表的筛选、页码和详情</span>
      </nav>
      <main class="app-content admin-stripe-surface"><RouterView /></main>
    </div>
    <TaskTray />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getVersion } from '../api/client'
import TaskTray from '../components/TaskTray.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
import { shortVersion } from '../utils/format'
import { normalizeAdminReturnTo } from '../utils/navigation'

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const navigationOpen = ref(false)
const fetchedVersion = ref('')
const fullVersion = computed(() => app.installation?.version || fetchedVersion.value || '')
const displayVersion = computed(() => shortVersion(fullVersion.value))
const currentTitle = computed(() => String(route.meta.title || '管理后台'))
const currentSection = computed(() => String(route.meta.section || 'ZBoard'))
const returnTarget = computed(() => normalizeAdminReturnTo(route.query.return_to))
const userInitial = computed(() => (app.user.email || 'Z').slice(0, 1).toUpperCase())
type NavigationLeaf = { to: string; label: string; icon?: string }
type NavigationItem = { to?: string; label: string; icon: string; children?: NavigationLeaf[] }
type NavigationGroup = { label: string; items: NavigationItem[] }
const navigationGroups: NavigationGroup[] = [
  { label: '工作台', items: [{ to: '/admin/dashboard', label: '运营工作台', icon: 'dashboard' }] },
  { label: '客户与支持', items: [
    { to: '/admin/users', label: '用户管理', icon: 'users' },
    { to: '/admin/subscriptions', label: '订阅管理', icon: 'plans' },
    { to: '/admin/fair-use', label: 'Fair Use 观测', icon: 'activity' },
    { to: '/admin/tickets', label: '工单中心', icon: 'ticket' }
  ] },
  { label: '商业管理', items: [
    { to: '/admin/plans', label: '商品与套餐', icon: 'plans' },
    { to: '/admin/subscription-templates', label: '订阅模板', icon: 'audit' },
    { to: '/admin/orders', label: '订单管理', icon: 'billing' }
  ] },
  { label: '基础设施', items: [
    { label: '资源与接入', icon: 'nodes', children: [
      { to: '/admin/nodes', label: '节点资产' },
      { to: '/admin/providers', label: '外部供应商' },
      { to: '/admin/dns-records', label: 'DNS 解析' }
    ] },
    { label: '服务与交付', icon: 'activity', children: [
      { to: '/admin/certificates', label: '免费证书' },
      { to: '/admin/protocols', label: '协议服务' },
      { to: '/admin/node-groups', label: '节点组' },
      { to: '/admin/traffic', label: '流量与对账' }
    ] }
  ] },
  { label: '系统运营', items: [
    { to: '/admin/tasks', label: '运营任务', icon: 'tasks' },
    { to: '/admin/operation-logs', label: '运行日志', icon: 'terminal' },
    { to: '/admin/audit-logs', label: '审计日志', icon: 'audit' },
    { to: '/admin/settings', label: '系统设置', icon: 'settings' },
    { to: '/admin/about', label: '关于 ZBoard', icon: 'info' }
  ] }
]

function isNavigationItemActive(path: string) {
  return route.path === path || route.path.startsWith(`${path}/`)
}

onMounted(async () => {
  try { fetchedVersion.value = (await getVersion())?.version || '' } catch { fetchedVersion.value = '' }
  await app.loadMe()
})
watch(() => route.fullPath, () => { navigationOpen.value = false })
function logout() { app.clear(); router.push('/') }
</script>

<style scoped>
.admin-brand-home { min-width: 0; display: flex; align-items: center; gap: 12px; text-decoration: none; }
.topbar-link { color: var(--primary); font-weight: 650; text-decoration: none; }
.topbar-link.muted-link { color: var(--muted); }.topbar-separator { width: 1px; height: 13px; background: var(--line-strong); }
.context-return-bar { min-height: 38px; display: flex; align-items: center; gap: 10px; padding: 6px 32px; border-bottom: 1px solid var(--line); background: var(--primary-soft); color: var(--muted); font-size: 10px; }
.context-return-bar a { display: inline-flex; align-items: center; gap: 5px; color: var(--primary); font-size: 11px; font-weight: 750; text-decoration: none; }
.context-return-bar a:hover { text-decoration: underline; }
.context-return-bar .ui-icon { transform: rotate(180deg); }
@media (max-width: 900px) {
  .context-return-bar { padding-inline: 18px; }
  .context-return-bar > span { display: none; }
}
</style>