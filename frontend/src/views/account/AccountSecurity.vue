<template>
  <section class="stack">
    <header><p class="page-eyebrow">账户安全</p><h1>登录与授权</h1><p>管理本地密码，以及由已安装插件提供的登录和授权方式。</p></header>
    <TransientFeedback :success="message" :error="error" />
    <section v-if="loaded" class="panel stack">
      <h2>{{ passwordSet ? '修改密码' : '设置本地密码' }}</h2>
      <p v-if="!passwordSet">请在第三方登录后五分钟内设置。过期后重新通过第三方登录即可继续。</p>
      <FormField v-if="passwordSet" v-slot="{ controlAttrs }" label="当前密码" name="account-current-password" required full><UiInput v-model="currentPassword" v-bind="controlAttrs" type="password" autocomplete="current-password" /></FormField>
      <FormField v-slot="{ controlAttrs }" label="新密码" name="identity-new-password" required full><UiInput v-model="newPassword" v-bind="controlAttrs" type="password" autocomplete="new-password" minlength="12" maxlength="72" /></FormField>
      <FormField v-slot="{ controlAttrs }" label="确认新密码" name="account-confirm-password" required full><UiInput v-model="repeatPassword" v-bind="controlAttrs" type="password" autocomplete="new-password" /></FormField>
      <UiButton type="button" :disabled="busy || !newPassword || !repeatPassword || (passwordSet && !currentPassword)" @click="setPassword">{{ passwordSet ? '修改密码' : '设置密码' }}</UiButton>
    </section>
    <PluginSlot name="account.security.identities" surface="account" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import FormField from '../../components/FormField.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import { fetchIdentityPasswordStatus, setupIdentityPassword } from '../../api/identities'
import { changeAccountPassword } from '../../api/accountSecurity'
import UiInput from '../../components/UiInput.vue'
import UiButton from '../../components/UiButton.vue'
import PluginSlot from '../../plugins/PluginSlot.vue'
const passwordSet = ref(true), loaded = ref(false), currentPassword = ref(''), repeatPassword = ref(''), newPassword = ref('')
const busy = ref(false), message = ref(''), error = ref('')
onMounted(async () => { try { passwordSet.value = (await fetchIdentityPasswordStatus()).password_set; loaded.value = true } catch { error.value = '账户安全信息加载失败，请刷新重试。' } })
async function setPassword() {
  if (busy.value) return
  if (newPassword.value !== repeatPassword.value) { error.value = '两次输入的新密码不一致。'; return }
  const changing = passwordSet.value
  busy.value = true; error.value = ''; message.value = ''
  try { if (passwordSet.value) await changeAccountPassword(currentPassword.value, newPassword.value); else await setupIdentityPassword(newPassword.value); passwordSet.value = true; message.value = changing ? '密码已修改。' : '本地密码已设置，可使用邮箱密码登录。' }
  catch (cause: any) { error.value = cause?.response?.data?.message || '修改失败，请确认当前密码及新密码长度；第三方授权过期时请重新登录。' }
  finally { currentPassword.value = ''; repeatPassword.value = ''; newPassword.value = ''; busy.value = false }
}
</script>
<style scoped>
.panel{padding:24px}
</style>
