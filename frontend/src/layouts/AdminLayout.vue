<template>
  <div class="app-shell" :class="{ 'nav-open': navigationOpen }">
    <button v-if="navigationOpen" class="nav-scrim" aria-label="关闭导航" @click="navigationOpen = false"></button>
    <aside class="app-sidebar">
      <div class="brand-block">
        <RouterLink class="admin-brand-home" to="/admin/dashboard"><span class="brand-mark">Z</span><div><strong>{{ app.siteName }}</strong><span>运营控制台</span></div></RouterLink>
        <button class="icon-button sidebar-close" type="button" aria-label="关闭导航" @click="navigationOpen = false"><UiIcon name="close" /></button>
      </div>

      <nav class="primary-nav" aria-label="管理端主导航">
        <section v-for="group in navigationGroups" :key="group.label" class="nav-group">
          <p>{{ group.label }}</p>
          <RouterLink v-for="item in group.items" :key="item.to" :to="item.to"><UiIcon :name="item.icon" /><span>{{ item.label }}</span></RouterLink>
        </section>
      </nav>

      <div class="sidebar-footer">
        <div class="account-summary">
          <span class="avatar">{{ userInitial }}</span>
          <div><strong>{{ app.user.email }}</strong><span>用户 · 管理员权限</span></div>
          <button class="icon-button" type="button" title="退出登录" aria-label="退出登录" @click="logout"><UiIcon name="logout" /></button>
        </div>
        <p class="build-version" :title="fullVersion">{{ displayVersion }}</p>
      </div>
    </aside>

    <div class="app-workspace">
      <header class="topbar">
        <div class="topbar-leading">
          <button class="icon-button menu-button" type="button" aria-label="打开导航" @click="navigationOpen = true"><UiIcon name="menu" /></button>
          <div><span class="topbar-context">{{ currentSection }}</span><strong>{{ currentTitle }}</strong></div>
        </div>
        <div class="topbar-status"><RouterLink class="topbar-link" to="/account">个人中心</RouterLink><span class="topbar-separator"></span><RouterLink class="topbar-link muted-link" to="/">查看站点</RouterLink></div>
      </header>
      <main class="app-content"><RouterView /></main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getVersion } from '../api/client'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
import { shortVersion } from '../utils/format'

const app = useAppStore()
const route = useRoute()
const router = useRouter()
const navigationOpen = ref(false)
const fetchedVersion = ref('')
const fullVersion = computed(() => app.installation?.version || fetchedVersion.value || '')
const displayVersion = computed(() => shortVersion(fullVersion.value))
const currentTitle = computed(() => String(route.meta.title || '管理后台'))
const currentSection = computed(() => String(route.meta.section || 'zboard'))
const userInitial = computed(() => (app.user.email || 'Z').slice(0, 1).toUpperCase())
const navigationGroups = [
  { label: '工作台', items: [{ to: '/admin/dashboard', label: '运营工作台', icon: 'dashboard' }] },
  { label: '客户与支持', items: [
    { to: '/admin/users', label: '用户管理', icon: 'users' },
    { to: '/admin/subscriptions', label: '订阅管理', icon: 'plans' },
    { to: '/admin/tickets', label: '工单中心', icon: 'ticket' }
  ] },
  { label: '商业管理', items: [
    { to: '/admin/plans', label: '商品与套餐', icon: 'plans' },
    { to: '/admin/orders', label: '订单管理', icon: 'billing' }
  ] },
  { label: '基础设施', items: [
    { to: '/admin/nodes', label: '节点资产', icon: 'nodes' },
    { to: '/admin/protocols', label: '协议服务', icon: 'activity' },
    { to: '/admin/node-groups', label: '节点组', icon: 'plans' },
    { to: '/admin/traffic', label: '流量与对账', icon: 'activity' }
  ] },
  { label: '系统运营', items: [
    { to: '/admin/tasks', label: '运营任务', icon: 'tasks' },
    { to: '/admin/audit-logs', label: '审计日志', icon: 'audit' },
    { to: '/admin/settings', label: '系统设置', icon: 'settings' }
  ] }
]

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
</style>
