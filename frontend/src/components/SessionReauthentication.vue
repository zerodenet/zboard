<template>
  <ModalDialog open title="登录已过期" description="在此使用同一账户的邮箱密码认证可保留未提交内容；付款等操作需要重新确认。" size="sm" busy>
    <p v-if="app.passwordLoginAvailable === false" class="muted">当前账户尚未设置邮箱密码，请使用原有第三方登录方式。</p>
    <form v-if="showPassword" ref="formElement" class="stack" novalidate @submit.prevent="submit">
      <FormField v-slot="{ controlAttrs }" label="邮箱地址" name="reauth-email" :error="errors.fields.email" required full>
        <UiInput v-model.trim="email" v-bind="controlAttrs" type="email" autocomplete="username" />
      </FormField>
      <FormField v-slot="{ controlAttrs }" label="密码" name="reauth-password" :error="errors.fields.password" required full>
        <UiInput v-model="password" v-bind="controlAttrs" type="password" autocomplete="current-password" />
      </FormField>
      <PageAlert v-if="errors.formError.value" tone="danger" title="认证未完成">{{ errors.formError.value }}</PageAlert>
      <p class="muted">更换账户会清除当前页面的草稿和缓存。</p>
      <UiButton type="submit" :loading="loading">重新认证</UiButton>
    </form>
    <UiButton v-else type="button" variant="secondary" @click="showPassword = true">{{ app.passwordLoginAvailable === false ? '使用其他账户的邮箱密码' : '我已设置邮箱密码' }}</UiButton>
    <div v-if="methods.length || methodsError" class="stack">
      <p v-if="methods.length">可用第三方登录：{{ methods.map(method => method.slot.title).join('、') }}。打开登录页后使用原有入口；离开当前页面会清除未提交内容。</p>
      <PageAlert v-if="methodsError" tone="warning" title="其他登录方式暂不可用">{{ methodsError }}</PageAlert>
      <UiButton v-if="methods.length" type="button" variant="secondary" :disabled="loading" @click="externalLogin">打开第三方登录入口</UiButton>
      <UiButton v-else type="button" variant="secondary" @click="loadMethods">重新检查登录方式</UiButton>
    </div>
    <PageAlert v-if="!methodsLoading && !methods.length && !methodsError && app.passwordLoginAvailable === false" tone="warning" title="暂无可用登录方式">当前没有可用的第三方登录入口，请联系管理员检查登录插件。</PageAlert>
    <UiButton type="button" variant="ghost" :disabled="loading" @click="logout">退出并返回首页</UiButton>
  </ModalDialog>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'
import { useFormErrors } from '../composables/useFormState'
import { collectFieldErrors, isEmail, isUtf8LengthInRange } from '../utils/validation'
import ModalDialog from './ModalDialog.vue'
import FormField from './FormField.vue'
import UiInput from './UiInput.vue'
import UiButton from './UiButton.vue'
import PageAlert from './PageAlert.vue'
import { fetchPluginSlots, type CatalogSlot } from '../api/plugins'
import { rememberAuthReturnPath } from '../utils/authReturnPath'

const app = useAppStore()
const router = useRouter()
const route = useRoute()
const email = ref(app.user.email)
const password = ref('')
const loading = ref(false)
const formElement = ref<HTMLElement | null>(null)
const errors = useFormErrors()
const showPassword = ref(app.passwordLoginAvailable === true)
const methods = ref<CatalogSlot[]>([])
const methodsError = ref('')
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
async function externalLogin() {
  const redirect = route.fullPath
  const accountID = app.user.id
  app.clear()
  rememberAuthReturnPath(redirect, accountID)
  await nextTick()
  await router.replace({ path: '/login', query: { redirect, reason: 'session-expired' } })
}
</script>
