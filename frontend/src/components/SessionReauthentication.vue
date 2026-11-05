<template>
  <ModalDialog open title="登录已过期" size="sm" :initial-focus-selector="showPassword ? '#reauth-password' : ''" busy>
    <div class="stack">
      <form v-if="showPassword" ref="formElement" class="stack" novalidate @submit.prevent="submit">
        <FormField v-slot="{ controlAttrs }" label="邮箱地址" name="reauth-email" :error="errors.fields.email" required full>
          <UiInput v-model.trim="email" v-bind="controlAttrs" type="email" autocomplete="username" inputmode="email" autocapitalize="none" enterkeyhint="next" />
        </FormField>
        <FormField v-slot="{ controlAttrs }" label="密码" name="reauth-password" :error="errors.fields.password" required full>
          <UiInput v-model="password" v-bind="controlAttrs" type="password" autocomplete="current-password" enterkeyhint="go" />
        </FormField>
        <PageAlert v-if="errors.formError.value" tone="danger" title="认证未完成">{{ errors.formError.value }}</PageAlert>
        <p v-if="changingAccount" class="muted">切换账户将清除未保存内容。</p>
        <UiButton type="submit" :loading="loading">重新登录</UiButton>
      </form>
      <div v-if="methods.length || methodsError" class="stack">
        <PageAlert v-if="externalRecoveryError" tone="warning" title="尚未离开当前页面">{{ externalRecoveryError }}</PageAlert>
        <PageAlert v-if="methodsError" tone="warning" title="其他登录方式暂不可用">{{ methodsError }}</PageAlert>
        <PluginFrame
          v-for="method in methods"
          :key="`${method.plugin_id}:${method.slot.id}:${method.generation}`"
          :plugin-id="method.plugin_id"
          :slot="method.slot"
          surface="public"
          :title="method.slot.title"
          :before-identity-login="prepareExternalLogin"
        />
        <UiButton v-if="methodsError" type="button" variant="secondary" @click="loadMethods">重新检查登录方式</UiButton>
      </div>
      <UiButton v-if="!showPassword" type="button" variant="secondary" @click="openPasswordLogin">邮箱密码登录</UiButton>
      <PageAlert v-if="!methodsLoading && !methods.length && !methodsError && app.passwordLoginAvailable === false" tone="warning" title="暂无可用登录方式">当前没有可用的第三方登录入口，请联系管理员检查登录插件。</PageAlert>
    </div>
    <template #footer><UiButton type="button" variant="ghost" :disabled="loading" @click="logout">退出并返回首页</UiButton></template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, watch, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'
import { useFormErrors } from '../composables/useFormState'
import { collectFieldErrors, isEmail, isUtf8LengthInRange } from '../utils/validation'
import ModalDialog from './ModalDialog.vue'
import FormField from './FormField.vue'
import UiInput from './UiInput.vue'
import UiButton from './UiButton.vue'
import PageAlert from './PageAlert.vue'
import PluginFrame from '../plugins/PluginFrame.vue'
import { fetchPluginSlots, type CatalogSlot } from '../api/plugins'
import { rememberAuthReturnPath } from '../utils/authReturnPath'
import { captureReauthenticationDrafts, safeReauthenticationRoute } from '../utils/reauthenticationDrafts'

const app = useAppStore()
const router = useRouter()
const route = useRoute()
const email = ref(app.user.email)
const password = ref('')
const loading = ref(false)
const formElement = ref<HTMLElement | null>(null)
const errors = useFormErrors()
watch(email, () => { if (!loading.value) errors.clear('email') }, { flush: 'sync' })
watch(password, () => { if (!loading.value) errors.clear('password') }, { flush: 'sync' })
const showPassword = ref(app.passwordLoginAvailable === true)
const changingAccount = computed(() => email.value.trim().toLowerCase() !== app.user.email.trim().toLowerCase())
const methods = ref<CatalogSlot[]>([])
const methodsError = ref('')
const externalRecoveryError = ref('')
const methodsLoading = ref(true)
async function loadMethods() {
  methodsLoading.value = true
  try {
    methods.value = await fetchPluginSlots('public', 'auth.login.methods')
    methodsError.value = ''
  } catch { methodsError.value = '无法读取登录方式，请检查网络后重试。' }
  finally { methodsLoading.value = false }
}
onMounted(loadMethods)

async function openPasswordLogin() {
  showPassword.value = true
  await nextTick()
  formElement.value?.querySelector<HTMLInputElement>('input[type="password"]')?.focus()
}

async function submit() {
  if (loading.value) return
  email.value = email.value.trim().toLowerCase()
  if (!await errors.applyValidation(collectFieldErrors({
    email: !isEmail(email.value) && '请输入有效邮箱。',
    password: !isUtf8LengthInRange(password.value, 12, 72) && '密码必须为 12–72 个 UTF-8 字节。',
  }), formElement)) return
  loading.value = true
  try {
    await app.reauthenticate(email.value, password.value)
    password.value = ''
    if (route.meta.requiresAdmin && !app.isAdmin) await router.replace('/account')
  } catch (cause) {
    password.value = ''
    await errors.applyApiError(cause, '无法完成认证，请检查登录信息与网络连接后重试。', formElement)
  } finally { loading.value = false }
}
async function logout() {
  app.clear()
  await nextTick()
  await router.replace('/')
}
function prepareExternalLogin(): boolean {
  if (loading.value) return false
  loading.value = true
  const redirect = safeReauthenticationRoute(route.fullPath)
  const accountID = app.user.id
  const recovery = captureReauthenticationDrafts(accountID, redirect)
  if (!recovery.ok) { externalRecoveryError.value = recovery.message; loading.value = false; return false }
  externalRecoveryError.value = ''
  app.clear({ preserveReauthenticationDrafts: true })
  rememberAuthReturnPath(redirect, accountID)
  return true
}
</script>
