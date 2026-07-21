<template>
  <div class="auth-shell">
    <aside class="auth-aside">
      <RouterLink class="auth-brand" to="/"><span class="brand-mark">Z</span><span>{{ store.siteName }}</span></RouterLink>
      <div class="auth-copy"><p class="page-eyebrow">WELCOME BACK</p><h1>继续管理你的<br />订阅与连接。</h1><p>登录后查看套餐、订单、订阅配置与流量使用，重要信息都在一个地方。</p></div>
      <p class="auth-footnote">套餐 · 订阅 · 流量，一站管理</p>
    </aside>
    <main class="auth-main">
      <form class="auth-card stack" @submit.prevent="submit">
        <div><p class="page-eyebrow">欢迎回来</p><h2>登录账户</h2><p>使用你的邮箱和密码继续。</p></div>
        <label class="field"><span>邮箱地址</span><input v-model.trim="email" type="email" autocomplete="email" placeholder="name@example.com" required /></label>
        <label class="field"><span>密码</span><input v-model="password" type="password" minlength="12" maxlength="72" autocomplete="current-password" placeholder="请输入密码" required /></label>
        <div v-if="error" class="alert alert-danger"><UiIcon name="alert" />{{ error }}</div>
        <button class="button login-button" :disabled="loading" type="submit">{{ loading ? '正在登录…' : '登录' }}</button>
        <RouterLink v-if="store.installation?.allow_registration" class="mode-switch" to="/register">还没有账户？立即注册</RouterLink>
        <RouterLink class="back-home" to="/">返回首页</RouterLink>
      </form>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { login } from '../api/client'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
const email = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')
const store = useAppStore()
const router = useRouter()
const route = useRoute()
async function submit() {
  loading.value = true; error.value = ''
  try {
    const result = await login(email.value, password.value)
    store.setToken(result.auth.token); store.setUser(result.user)
    const requested = typeof route.query.redirect === 'string' ? route.query.redirect : ''
    const accountRequest = requested === '/account' || requested.startsWith('/account/')
    const adminRequest = requested === '/admin' || requested.startsWith('/admin/')
    const safeRequested = accountRequest || (result.user.is_admin && adminRequest)
    await router.push(safeRequested ? requested : result.user.is_admin ? '/admin/dashboard' : '/account')
  } catch (e: any) { error.value = e?.response?.data?.message || '登录失败，请检查邮箱和密码。' }
  finally { loading.value = false }
}
</script>

<style scoped>
.auth-brand { text-decoration: none; }.auth-footnote { position: relative; z-index: 1; margin: 0; color: #7186a3; font-size: 12px; }.login-button { width: 100%; min-height: 44px; }.mode-switch,.back-home { text-align: center; text-decoration: none; font-size: 13px; font-weight: 650; }.mode-switch { color: var(--primary); }.back-home { color: var(--muted); font-weight: 500; }
</style>
