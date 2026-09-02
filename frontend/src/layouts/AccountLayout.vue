<template>
  <div class="account-shell">
    <header class="account-header">
      <RouterLink class="public-brand" to="/"><img v-if="app.siteProfile.logo" class="account-logo" :src="app.siteProfile.logo" :alt="app.siteName" /><span v-else class="brand-mark">{{ brandInitial }}</span><strong>{{ app.siteName }}</strong></RouterLink>
      <UiButton variant="ghost" icon class="icon-button account-menu" type="button" :aria-label="menuOpen ? '关闭用户导航' : '打开用户导航'" :aria-expanded="menuOpen" aria-controls="account-navigation" @click="menuOpen = !menuOpen"><UiIcon :name="menuOpen ? 'close' : 'menu'" /></UiButton>
      <nav id="account-navigation" :class="{ open: menuOpen }" aria-label="用户中心导航">
        <RouterLink to="/account"><UiIcon name="dashboard" />概览</RouterLink>
        <RouterLink to="/account/plans"><UiIcon name="plans" />购买套餐</RouterLink>
        <RouterLink to="/account/orders"><UiIcon name="billing" />我的订单</RouterLink>
        <RouterLink to="/account/subscription"><UiIcon name="key" />订阅配置</RouterLink>
        <RouterLink to="/account/traffic"><UiIcon name="activity" />流量明细</RouterLink>
        <RouterLink to="/account/announcements"><UiIcon name="info" />公告中心<span v-if="app.announcementUnreadCount" class="nav-count">{{ app.announcementUnreadCount > 99 ? '99+' : app.announcementUnreadCount }}</span></RouterLink>
        <RouterLink to="/account/tickets"><UiIcon name="ticket" />我的工单</RouterLink>
        <RouterLink v-if="app.isAdmin" to="/admin/dashboard"><UiIcon name="shield" />管理后台</RouterLink>
      </nav>
      <div class="account-identity"><span class="avatar">{{ userInitial }}</span><div><strong>{{ app.user.email }}</strong><span v-if="app.isAdmin">已授予管理员权限</span></div><UiButton variant="ghost" icon class="icon-button" type="button" aria-label="退出登录" title="退出登录" @click="logout"><UiIcon name="logout" /></UiButton></div>
    </header>
    <main class="account-content"><RouterView /></main>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
const app = useAppStore()
const route = useRoute()
const router = useRouter()
const menuOpen = ref(false)
const userInitial = computed(() => (app.user.email || 'U').slice(0, 1).toUpperCase())
const brandInitial = computed(() => Array.from(app.siteName.trim())[0]?.toUpperCase() || 'Z')
watch(() => route.fullPath, () => { menuOpen.value = false })
function logout() { app.clear(); router.push('/') }
</script>

<style scoped>
.account-logo { display: block; width: auto; max-width: 150px; height: 32px; object-fit: contain; }
.nav-count { min-width: 18px; height: 18px; display: inline-grid; place-items: center; margin-left: auto; padding: 0 5px; border-radius: 999px; background: var(--danger); color: var(--text-inverse); font-size: 9px; font-weight: 800; }
</style>
