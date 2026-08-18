<template>
  <div class="public-shell">
    <header class="public-header">
      <RouterLink class="public-brand" to="/"><span class="brand-mark">Z</span><strong>{{ app.siteName }}</strong></RouterLink>
      <UiButton variant="ghost" icon class="icon-button public-menu" type="button" :aria-label="menuOpen ? '关闭站点导航' : '打开站点导航'" :aria-expanded="menuOpen" aria-controls="public-navigation" @click="menuOpen = !menuOpen"><UiIcon :name="menuOpen ? 'close' : 'menu'" /></UiButton>
      <nav id="public-navigation" :class="{ open: menuOpen }" aria-label="站点导航"><RouterLink to="/" @click="menuOpen = false">首页</RouterLink><RouterLink to="/pricing" @click="menuOpen = false">套餐</RouterLink></nav>
      <div class="public-actions">
        <RouterLink v-if="app.isAuthenticated" class="button button-secondary button-sm" :to="landingPath">进入{{ app.isAdmin ? '管理后台' : '用户中心' }}</RouterLink>
        <template v-else><RouterLink class="button button-ghost button-sm" to="/login">登录</RouterLink><RouterLink v-if="app.installation?.allow_registration" class="button button-sm" to="/register">免费注册</RouterLink></template>
      </div>
    </header>
    <main><RouterView /></main>
    <footer class="public-footer"><div><RouterLink class="public-brand" to="/"><span class="brand-mark">Z</span><strong>{{ app.siteName }}</strong></RouterLink><p>简单、透明地管理你的网络订阅。</p></div><div><RouterLink to="/pricing">套餐</RouterLink><RouterLink v-if="app.isAuthenticated" :to="landingPath">进入{{ app.isAdmin ? '管理后台' : '用户中心' }}</RouterLink><RouterLink v-else to="/login">登录</RouterLink><span>{{ versionLabel }}</span></div></footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getVersion } from '../api/client'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
import { shortVersion } from '../utils/format'
const app = useAppStore()
const version = ref('')
const menuOpen = ref(false)
const landingPath = computed(() => app.isAdmin ? '/admin/dashboard' : '/account')
const versionLabel = computed(() => shortVersion(version.value || app.installation?.version))
onMounted(async () => { try { version.value = (await getVersion())?.version || '' } catch { version.value = '' } })
</script>
