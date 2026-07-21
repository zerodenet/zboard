<template>
  <div class="account-shell">
    <header class="account-header">
      <RouterLink class="public-brand" to="/"><span class="brand-mark">Z</span><strong>{{ app.siteName }}</strong></RouterLink>
      <button class="icon-button account-menu" type="button" aria-label="打开用户导航" @click="menuOpen = !menuOpen"><UiIcon :name="menuOpen ? 'close' : 'menu'" /></button>
      <nav :class="{ open: menuOpen }" aria-label="用户中心导航">
        <RouterLink to="/account"><UiIcon name="dashboard" />概览</RouterLink>
        <RouterLink to="/account/plans"><UiIcon name="plans" />购买套餐</RouterLink>
        <RouterLink to="/account/orders"><UiIcon name="billing" />我的订单</RouterLink>
        <RouterLink to="/account/subscription"><UiIcon name="key" />订阅配置</RouterLink>
        <RouterLink to="/account/traffic"><UiIcon name="activity" />流量明细</RouterLink>
        <RouterLink to="/account/tickets"><UiIcon name="ticket" />我的工单</RouterLink>
        <RouterLink v-if="app.isAdmin" to="/admin/dashboard"><UiIcon name="shield" />管理后台</RouterLink>
      </nav>
      <div class="account-identity"><span class="avatar">{{ userInitial }}</span><div><strong>{{ app.user.email }}</strong><span v-if="app.isAdmin">已授予管理员权限</span></div><button class="icon-button" type="button" aria-label="退出登录" title="退出登录" @click="logout"><UiIcon name="logout" /></button></div>
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
watch(() => route.fullPath, () => { menuOpen.value = false })
function logout() { app.clear(); router.push('/') }
</script>
