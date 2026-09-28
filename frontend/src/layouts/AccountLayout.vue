<template>
  <div class="account-shell">
    <header class="account-header" @keydown.esc="menuOpen = false">
      <RouterLink class="public-brand" to="/"><img v-if="app.siteProfile.logo" class="account-logo" :src="app.siteProfile.logo" :alt="app.siteName" /><span v-else class="brand-mark">{{ brandInitial }}</span><strong>{{ app.siteName }}</strong></RouterLink>
      <UiButton variant="ghost" icon class="icon-button account-menu" type="button" :aria-label="menuOpen ? '关闭用户导航' : '打开用户导航'" :aria-expanded="menuOpen" aria-controls="account-navigation" @click="menuOpen = !menuOpen"><UiIcon :name="menuOpen ? 'close' : 'menu'" /></UiButton>
      <nav id="account-navigation" :class="{ open: menuOpen }" aria-label="用户中心导航">
        <NavigationLinks surface="account" @select-page="menuOpen = false" />
        <button v-if="menus.error.value" type="button" @click="menus.load()">重新加载菜单</button>
      </nav>
      <div class="account-identity"><span class="avatar">{{ userInitial }}</span><div><strong>{{ app.user.email }}</strong><span v-if="app.isAdmin">已授予管理员权限</span></div><UiButton variant="ghost" icon class="icon-button" type="button" aria-label="退出登录" title="退出登录" @click="logout"><UiIcon name="logout" /></UiButton></div>
    </header>
    <main class="account-content"><NavigationPage :available="menus.pageAvailable.value" :loading="menus.pageLoading.value" :error="menus.pageError.value" @retry="menus.load()"><RouterView /></NavigationPage></main>
  </div>
</template>

<script setup lang="ts">
import NavigationLinks from '../components/NavigationLinks.vue'
import NavigationPage from '../components/NavigationPage.vue'
import { useNavigation } from '../stores/navigation'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UiIcon from '../components/UiIcon.vue'
import UiButton from '../components/UiButton.vue'
import { useAppStore } from '../stores/app'
const app = useAppStore()
const menus = useNavigation('account')
const route = useRoute()
const router = useRouter()
const menuOpen = ref(false)
const userInitial = computed(() => (app.user.email || 'U').slice(0, 1).toUpperCase())
const brandInitial = computed(() => Array.from(app.siteName.trim())[0]?.toUpperCase() || 'Z')
watch(() => route.fullPath, () => { menuOpen.value = false })
function logout() { app.clear(); router.push('/') }
</script>

<style scoped>
.account-logo { display: block; width: auto; max-width: 150px; height: 34px; object-fit: contain; }
.nav-count { min-width: 18px; height: 18px; display: inline-grid; place-items: center; margin-left: auto; padding: 0 5px; border-radius: 999px; background: var(--danger); color: var(--text-inverse); font-size: 9px; font-weight: 800; }
</style>
