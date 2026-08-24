<template>
  <div class="auth-shell">
    <aside class="auth-aside">
      <RouterLink class="auth-brand" to="/"><img v-if="store.siteProfile.logoDark || store.siteProfile.logo" class="auth-logo" :src="store.siteProfile.logoDark || store.siteProfile.logo" :alt="store.siteName" /><span v-else class="brand-mark">{{ store.siteName.charAt(0).toUpperCase() || 'Z' }}</span><span>{{ store.siteName }}</span></RouterLink>
      <div class="auth-copy"><p class="page-eyebrow">WELCOME BACK</p><h1>继续管理你的<br />订阅与连接。</h1><p>登录后查看套餐、订单、订阅配置与流量使用，重要信息都在一个地方。</p></div>
      <p class="auth-footnote">套餐 · 订阅 · 流量，一站管理</p>
    </aside>
    <main class="auth-main">
      <form ref="formElement" class="auth-card stack" novalidate @submit.prevent="submit">
        <div><p class="page-eyebrow">欢迎回来</p><h2>登录账户</h2><p>使用你的邮箱和密码继续。</p></div>
        <FormField v-slot="{ controlAttrs }" label="邮箱地址" name="login-email" :error="formErrors.fields.email" required full><UiInput v-model.trim="email" v-bind="controlAttrs" type="email" autocomplete="email" placeholder="name@example.com" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="密码" name="login-password" hint="密码长度为 12–72 个 UTF-8 字节。" :error="formErrors.fields.password" required full><UiInput v-model="password" v-bind="controlAttrs" type="password" minlength="12" maxlength="72" autocomplete="current-password" placeholder="请输入密码" /></FormField>
        <PageAlert v-if="formErrors.formError.value" tone="danger" title="登录未完成">{{ formErrors.formError.value }}</PageAlert>
        <UiButton class="login-button" :loading="loading" type="submit">登录</UiButton>
        <AuthLegalLinks context="login" />
        <RouterLink v-if="store.installation?.allow_registration" class="mode-switch" to="/register">还没有账户？立即注册</RouterLink>
        <RouterLink class="back-home" to="/">返回首页</RouterLink>
      </form>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { login } from '../api/client'
import AuthLegalLinks from '../components/AuthLegalLinks.vue'
import FormField from '../components/FormField.vue'
import PageAlert from '../components/PageAlert.vue'
import { useFormErrors } from '../composables/useFormState'
import { useAppStore } from '../stores/app'
import { collectFieldErrors, isEmail, isUtf8LengthInRange } from '../utils/validation'
const email = ref('')
const password = ref('')
const loading = ref(false)
const formElement = ref<HTMLElement | null>(null)
const formErrors = useFormErrors()
const store = useAppStore()
const router = useRouter()
const route = useRoute()
watch(email, () => formErrors.clear('email'))
watch(password, () => formErrors.clear('password'))
async function submit() {
  email.value = email.value.trim().toLowerCase()
  const valid = await formErrors.applyValidation(collectFieldErrors({
    email: !isEmail(email.value) && '请输入不超过 128 个 UTF-8 字节的有效邮箱。',
    password: !isUtf8LengthInRange(password.value, 12, 72) && '密码必须为 12–72 个 UTF-8 字节。',
  }), formElement, '请更正标记字段后再登录。')
  if (!valid) return
  loading.value = true
  try {
    const result = await login(email.value, password.value)
    store.setToken(result.auth.token); store.setUser(result.user)
    const requested = typeof route.query.redirect === 'string' ? route.query.redirect : ''
    const accountRequest = requested === '/account' || requested.startsWith('/account/')
    const adminRequest = requested === '/admin' || requested.startsWith('/admin/')
    const safeRequested = accountRequest || (result.user.is_admin && adminRequest)
    await router.push(safeRequested ? requested : result.user.is_admin ? '/admin/dashboard' : '/account')
  } catch (e: any) { await formErrors.applyApiError(e, '登录失败，请检查邮箱和密码。', formElement, { email: 'email', password: 'password' }) }
  finally { loading.value = false }
}
</script>

<style scoped>
.auth-brand { text-decoration: none; }.auth-logo { width: auto; max-width: 180px; height: 36px; object-fit: contain; }.auth-footnote { position: relative; z-index: 1; margin: 0; color: var(--auth-footnote); font-size: 12px; }.login-button { width: 100%; min-height: 44px; }.mode-switch,.back-home { text-align: center; text-decoration: none; font-size: 13px; font-weight: 650; }.mode-switch { color: var(--primary); }.back-home { color: var(--muted); font-weight: 500; }
</style>
