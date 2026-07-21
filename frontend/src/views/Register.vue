<template>
  <div class="auth-shell">
    <aside class="auth-aside">
      <RouterLink class="auth-brand" to="/"><span class="brand-mark">Z</span><span>{{ store.siteName }}</span></RouterLink>
      <div class="auth-copy"><p class="page-eyebrow">GET STARTED</p><h1>一个账户，管理<br />所有订阅。</h1><p>注册后可以选购套餐、查看订单、生成订阅链接并追踪自己的流量使用。</p></div>
      <p class="auth-footnote">注册即用 · 独立用户空间</p>
    </aside>
    <main class="auth-main">
      <form class="auth-card stack" @submit.prevent="submit">
        <div><p class="page-eyebrow">创建账户</p><h2>注册新用户</h2><p>密码长度需要为 12–72 个 UTF-8 字节。</p></div>
        <label class="field"><span>邮箱地址</span><input v-model.trim="email" type="email" autocomplete="email" placeholder="name@example.com" required /></label>
        <label class="field"><span>密码</span><input v-model="password" type="password" minlength="12" maxlength="72" autocomplete="new-password" required /></label>
        <label class="field"><span>确认密码</span><input v-model="confirmPassword" type="password" minlength="12" maxlength="72" autocomplete="new-password" required /></label>
        <div v-if="error" class="alert alert-danger"><UiIcon name="alert" />{{ error }}</div>
        <button class="button login-button" :disabled="loading" type="submit">{{ loading ? '正在创建…' : '创建账户' }}</button>
        <RouterLink class="mode-switch" to="/login">已有账户？返回登录</RouterLink>
        <RouterLink class="back-home" to="/">返回首页</RouterLink>
      </form>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { register } from '../api/client'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
const email = ref(''); const password = ref(''); const confirmPassword = ref(''); const loading = ref(false); const error = ref('')
const store = useAppStore(); const router = useRouter()
async function submit() {
  error.value = ''
  if (password.value !== confirmPassword.value) { error.value = '两次输入的密码不一致。'; return }
  loading.value = true
  try { const result = await register({ email: email.value, password: password.value }); store.setToken(result.auth.token); store.setUser(result.user); await router.push('/account') }
  catch (e: any) { error.value = e?.response?.data?.message || '注册失败，请稍后重试。' }
  finally { loading.value = false }
}
</script>

<style scoped>
.auth-brand { text-decoration: none; }.auth-footnote { position: relative; z-index: 1; margin: 0; color: #7186a3; font-size: 12px; }.login-button { width: 100%; min-height: 44px; }.mode-switch,.back-home { text-align: center; text-decoration: none; font-size: 13px; font-weight: 650; }.mode-switch { color: var(--primary); }.back-home { color: var(--muted); font-weight: 500; }
</style>
