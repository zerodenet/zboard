<template>
  <div class="auth-shell">
    <aside class="auth-aside">
      <RouterLink class="auth-brand" to="/"><img v-if="store.siteProfile.logoDark || store.siteProfile.logo" class="auth-logo" :src="store.siteProfile.logoDark || store.siteProfile.logo" :alt="store.siteName" /><span v-else class="brand-mark">{{ store.siteName.charAt(0).toUpperCase() || 'Z' }}</span><span>{{ store.siteName }}</span></RouterLink>
      <div class="auth-copy"><p class="page-eyebrow">GET STARTED</p><h1>一个账户，管理<br />所有订阅。</h1><p>注册后可以选购套餐、查看订单、生成订阅链接并追踪自己的流量使用。</p></div>
      <p class="auth-footnote">注册即用 · 独立用户空间</p>
    </aside>
    <main class="auth-main">
      <form ref="formElement" class="auth-card stack" novalidate @submit.prevent="submit">
        <div><p class="page-eyebrow">创建账户</p><h2>注册新用户</h2><p>密码长度需要为 12–72 个 UTF-8 字节。</p></div>
        <FormField v-slot="{ controlAttrs }" label="邮箱地址" name="register-email" :error="formErrors.fields.email" required full><UiInput v-model.trim="email" v-bind="controlAttrs" type="email" autocomplete="email" placeholder="name@example.com" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="密码" name="register-password" hint="长度为 12–72 个 UTF-8 字节。" :error="formErrors.fields.password" required full><UiInput v-model="password" v-bind="controlAttrs" type="password" minlength="12" maxlength="72" autocomplete="new-password" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="确认密码" name="register-confirm-password" hint="再次输入相同密码。" :error="formErrors.fields.confirm_password" required full><UiInput v-model="confirmPassword" v-bind="controlAttrs" type="password" minlength="12" maxlength="72" autocomplete="new-password" /></FormField>
        <PageAlert v-if="formErrors.formError.value" tone="danger" title="注册未完成">{{ formErrors.formError.value }}</PageAlert>
        <UiButton class="login-button" :loading="loading" type="submit">创建账户</UiButton>
        <AuthLegalLinks context="register" />
        <RouterLink class="mode-switch" to="/login">已有账户？返回登录</RouterLink>
        <RouterLink class="back-home" to="/">返回首页</RouterLink>
      </form>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { register } from '../api/client'
import AuthLegalLinks from '../components/AuthLegalLinks.vue'
import FormField from '../components/FormField.vue'
import PageAlert from '../components/PageAlert.vue'
import { useFormErrors } from '../composables/useFormState'
import { useAppStore } from '../stores/app'
import { collectFieldErrors, isEmail, isUtf8LengthInRange } from '../utils/validation'
const email = ref(''); const password = ref(''); const confirmPassword = ref(''); const loading = ref(false)
const formElement = ref<HTMLElement | null>(null); const formErrors = useFormErrors()
const store = useAppStore(); const router = useRouter()
watch(email, () => formErrors.clear('email')); watch(password, () => formErrors.clear('password')); watch(confirmPassword, () => formErrors.clear('confirm_password'))
async function submit() {
  email.value = email.value.trim().toLowerCase()
  const valid = await formErrors.applyValidation(collectFieldErrors({
    email: !isEmail(email.value) && '请输入不超过 128 个 UTF-8 字节的有效邮箱。',
    password: !isUtf8LengthInRange(password.value, 12, 72) && '密码必须为 12–72 个 UTF-8 字节。',
    confirm_password: password.value !== confirmPassword.value && '两次输入的密码不一致。',
  }), formElement, '请更正标记字段后再注册。')
  if (!valid) return
  loading.value = true
  try { const result = await register({ email: email.value, password: password.value }); store.setToken(result.auth.token); store.setUser(result.user); await router.push('/account') }
  catch (e: any) { await formErrors.applyApiError(e, '注册失败，请稍后重试。', formElement, { email: 'email', password: 'password' }) }
  finally { loading.value = false }
}
</script>

<style scoped>
.auth-brand { text-decoration: none; }.auth-logo { width: auto; max-width: 180px; height: 36px; object-fit: contain; }.auth-footnote { position: relative; z-index: 1; margin: 0; color: var(--auth-footnote); font-size: 12px; }.login-button { width: 100%; min-height: 44px; }.mode-switch,.back-home { text-align: center; text-decoration: none; font-size: 13px; font-weight: 650; }.mode-switch { color: var(--primary); }.back-home { color: var(--muted); font-weight: 500; }
</style>
