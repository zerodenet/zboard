<template>
  <section class="stack">
    <header><p class="page-eyebrow">账户安全</p><h1>登录与授权</h1><p>管理本地密码，以及由已安装插件提供的登录和授权方式。</p></header>
    <TransientFeedback :success="message" :error="error" />
    <section v-if="!passwordSet" class="panel stack">
      <h2>设置本地密码</h2><p>请在第三方登录后五分钟内设置。过期后重新通过第三方登录即可继续。</p>
      <FormField v-slot="{ controlAttrs }" label="新密码" name="identity-new-password" required full><UiInput v-model="newPassword" v-bind="controlAttrs" type="password" autocomplete="new-password" minlength="12" maxlength="72" /></FormField>
      <UiButton type="button" :disabled="busy || !newPassword" @click="setPassword">设置密码</UiButton>
    </section>
    <PluginSlot name="account.security.identities" surface="account" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import FormField from '../../components/FormField.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import { fetchIdentityPasswordStatus, setupIdentityPassword } from '../../api/identities'
import PluginSlot from '../../plugins/PluginSlot.vue'
const passwordSet = ref(true), newPassword = ref('')
const busy = ref(false), message = ref(''), error = ref('')
onMounted(async () => { try { passwordSet.value = (await fetchIdentityPasswordStatus()).password_set } catch { error.value = '账户安全信息加载失败，请刷新重试。' } })
async function setPassword() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { await setupIdentityPassword(newPassword.value); passwordSet.value = true; message.value = '本地密码已设置，可使用邮箱密码登录。' }
  catch { error.value = '设置失败，请确认密码为 12–72 个 UTF-8 字节；授权过期时请重新通过第三方登录。' }
  finally { newPassword.value = ''; busy.value = false }
}
</script>
<style scoped>
.panel{padding:24px}
</style>
