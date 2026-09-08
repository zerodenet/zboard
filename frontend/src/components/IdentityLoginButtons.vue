<template>
  <section v-if="providers.length" class="identity-login" aria-label="第三方登录">
    <p>已绑定第三方账号？</p>
    <UiButton v-for="provider in providers" :key="provider.id" type="button" variant="secondary" :disabled="busy" @click="start(provider.id)">使用 {{ provider.name }}</UiButton>
    <p v-if="error" role="alert">{{ error }}</p>
    <small>首次使用请先通过邮箱登录，在账户安全中绑定。</small>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchIdentityProviders, navigateToIdentityProvider, startIdentityLogin, type IdentityProvider } from '../api/identities'
const providers = ref<IdentityProvider[]>([])
const busy = ref(false)
const error = ref('')
onMounted(async () => { try { providers.value = await fetchIdentityProviders() } catch { providers.value = [] } })
async function start(id: string) {
  busy.value = true; error.value = ''
  try { navigateToIdentityProvider((await startIdentityLogin(id)).authorization_url) }
  catch { error.value = '第三方登录暂不可用，请重试或使用邮箱登录。'; busy.value = false }
}
</script>
<style scoped>
.identity-login{display:grid;gap:10px;border-top:1px solid var(--line);padding-top:16px}.identity-login p{margin:0}.identity-login small{color:var(--muted)}
</style>
