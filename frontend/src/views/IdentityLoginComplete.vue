<template>
  <main class="auth-main identity-complete-shell"><section class="auth-card stack">
    <h1>{{ registering ? '补充注册邮箱' : failed ? '第三方登录未完成' : '正在完成身份验证' }}</h1>
    <p :role="failed ? 'alert' : 'status'">{{ statusText }}</p>
    <form v-if="registering" ref="formElement" class="stack" novalidate @submit.prevent="completeRegistration">
      <FormField v-slot="{ controlAttrs }" label="邮箱地址" name="identity-register-email" :error="formErrors.fields.email" required full><UiInput v-model.trim="email" v-bind="controlAttrs" type="email" autocomplete="email" maxlength="128" /></FormField>
      <UiButton type="button" variant="secondary" :disabled="busy || cooldown > 0" @click="sendCode">{{ cooldown > 0 ? `${cooldown} 秒后重发` : '发送邮箱验证码' }}</UiButton>
      <FormField v-slot="{ controlAttrs }" label="邮箱验证码" name="identity-register-code" :error="formErrors.fields.verification_code" required full><UiInput v-model.trim="code" v-bind="controlAttrs" inputmode="numeric" maxlength="6" autocomplete="one-time-code" /></FormField>
      <UiButton type="submit" :disabled="busy">验证邮箱并注册</UiButton>
      <PageAlert v-if="formErrors.formError.value" tone="danger" title="注册未完成">{{ formErrors.formError.value }}</PageAlert>
      <AuthLegalLinks context="register" />
    </form>
    <template v-if="failed"><RouterLink class="identity-retry" to="/login">返回登录并重试</RouterLink><RouterLink class="identity-bind" to="/account/security">已有账户？登录后绑定</RouterLink></template>
  </section></main>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { finishIdentityLogin, requestIdentityRegistrationCode, type IdentityLoginResult } from '../api/identities'
import { useFormErrors } from '../composables/useFormState'
import { collectFieldErrors, isEmail } from '../utils/validation'
import PageAlert from '../components/PageAlert.vue'
import FormField from '../components/FormField.vue'
import AuthLegalLinks from '../components/AuthLegalLinks.vue'
import { useAppStore } from '../stores/app'
const route = useRoute(), router = useRouter(), store = useAppStore()
const failed = ref(false), registering = ref(false), busy = ref(false), email = ref(''), code = ref(''), cooldown = ref(0)
const formElement = ref<HTMLElement | null>(null), formErrors = useFormErrors()
watch(email, () => formErrors.clear('email')); watch(code, () => formErrors.clear('verification_code'))
const statusText = ref('正在向 ZBoard 确认登录结果…')
let timer: ReturnType<typeof setInterval> | undefined
async function accept(result: IdentityLoginResult) {
  if (result.registration_required) { registering.value = true; email.value = result.email || ''; statusText.value = '第三方未提供已验证邮箱。请在五分钟内完成邮箱验证，由 ZBoard 创建账户。'; return }
  if (result.linked) { await router.replace({path:'/account/security',query:{linked:'1'}}); return }
  if (!result.auth?.token || !result.user) throw new Error('missing login result')
  store.setToken(result.auth.token); store.setUser(result.user)
  await store.loadSystemStatus(true).catch(() => undefined)
  await router.replace(result.user.is_admin ? '/admin/dashboard' : '/account')
}
const callbackErrors: Record<string, string> = {
  invalid_response: '授权服务返回的信息不完整，请重新发起登录。',
  state_expired: '登录验证已过期，或授权返回时使用了不同的浏览器。请在同一浏览器中重新登录。',
  authorization_denied: '你没有完成第三方授权。请重新尝试并确认授权。',
  provider_unavailable: '第三方登录服务暂不可用，请稍后重试或使用邮箱登录。',
  config_changed: '授权过程中第三方登录配置发生变化，请重新发起登录。',
  issuer_mismatch: '第三方授权来源与站点配置不一致，请联系管理员检查提供方设置。',
  registration_closed: '该第三方账号尚未绑定本站账号，且本站目前关闭注册。请先用原方式登录，再到账户安全中绑定。',
  account_unavailable: '本站账号状态或绑定验证已变化。请用原方式重新登录；若仍失败，请联系管理员。',
  binding_conflict: '该第三方账号已绑定其他本站账号，请确认使用的第三方账号。',
  verification_failed: '本站未能验证第三方身份。请重试；若持续失败，请联系管理员检查 OAuth 插件与提供方配置。',
}
function fail(message = '第三方登录未完成。请重试；已有本站账号请先用原方式登录，再到账户安全中绑定。') {
  failed.value = true; registering.value = false; statusText.value = message
}
onMounted(async () => {
  const code = typeof route.query.error === 'string' ? route.query.error : ''
  if (code) { fail(callbackErrors[code]); return }
  try { await accept(await finishIdentityLogin()) } catch { fail() }
})
async function sendCode() {
  if (busy.value || cooldown.value > 0) return
  if (!await formErrors.applyValidation(collectFieldErrors({email: !isEmail(email.value) && '请输入有效邮箱。'}), formElement)) return
  busy.value = true
  try { const result = await requestIdentityRegistrationCode(email.value); statusText.value = '验证码已发送，请检查邮箱。'; cooldown.value = result.resend_after; if (timer) clearInterval(timer); timer = setInterval(() => { cooldown.value = Math.max(0, cooldown.value - 1); if (!cooldown.value && timer) clearInterval(timer) }, 1000) }
  catch (error) { await formErrors.applyApiError(error, '验证码发送失败，请确认邮箱未被注册、本站已配置邮件服务且授权未过期。', formElement, {email:'email'}) }
  finally { busy.value = false }
}
async function completeRegistration() {
  if (busy.value) return
  if (!await formErrors.applyValidation(collectFieldErrors({email:!isEmail(email.value) && '请输入有效邮箱。',verification_code:!/^\d{6}$/.test(code.value) && '请输入 6 位验证码。'}), formElement)) return
  busy.value = true
  try { await accept(await finishIdentityLogin({email:email.value,verification_code:code.value})) } catch { fail() }
  finally { busy.value = false; code.value = '' }
}
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>
<style scoped>
.identity-complete-shell { min-height: 100svh; }
.identity-complete-shell > .auth-card { width: min(480px, 100%); }
.identity-retry, .identity-bind { width: fit-content; font-size: 13px; text-decoration: none; }
.identity-retry { color: var(--primary); font-weight: 650; }
.identity-bind { color: var(--muted); }
.identity-retry:hover, .identity-bind:hover { text-decoration: underline; }
</style>
