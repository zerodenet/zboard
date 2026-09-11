<template>
  <section class="stack">
    <header><p class="page-eyebrow">账户安全</p><h1>第三方登录</h1><p>绑定后可使用第三方账号登录。绑定和解绑都需要确认当前 ZBoard 密码；第三方注册用户可在此设置本地密码。</p></header>
    <TransientFeedback :success="message" :error="error" />
    <section v-if="!passwordSet" class="panel stack">
      <h2>设置本地密码</h2><p>请在第三方登录后五分钟内设置。过期后重新通过第三方登录即可继续。</p>
      <FormField v-slot="{ controlAttrs }" label="新密码" name="identity-new-password" required full><UiInput v-model="newPassword" v-bind="controlAttrs" type="password" autocomplete="new-password" minlength="12" maxlength="72" /></FormField>
      <UiButton type="button" :disabled="busy || !newPassword" @click="setPassword">设置密码</UiButton>
    </section>
    <section class="panel stack">
      <FormField v-slot="{ controlAttrs }" label="当前密码" name="identity-password" required full><UiInput v-model="password" v-bind="controlAttrs" type="password" autocomplete="current-password" maxlength="72" /></FormField>
      <h2>已绑定账号</h2>
      <p v-if="!identities.length">尚未绑定第三方账号。</p>
      <div v-for="identity in identities" :key="identity.id" class="identity-row">
        <div class="identity-summary">
          <strong>{{ providerName(identity) }}</strong>
          <dl class="identity-details">
            <div><dt>第三方账号标识</dt><dd class="mono">{{ identity.subject }}</dd></div>
            <div><dt>提供方</dt><dd>{{ identity.provider_id || identity.plugin_id }}</dd></div>
            <div><dt>插件来源</dt><dd>{{ identity.publisher }}</dd></div>
            <div><dt>插件 ID</dt><dd class="mono">{{ pluginBaseID(identity) }}</dd></div>
            <div><dt>身份签发方</dt><dd class="mono">{{ identity.issuer }}</dd></div>
            <div><dt>绑定时间</dt><dd><TimeBadge :value="identity.created_at" /></dd></div>
          </dl>
        </div>
        <UiButton type="button" variant="secondary" :disabled="busy || !passwordSet || !password" @click="unlink(identity.id)">解绑</UiButton>
      </div>
      <h2>可用提供方</h2>
      <p v-if="!providers.length">当前没有可用的第三方登录提供方。</p>
      <div v-for="provider in providers" :key="provider.id" class="identity-row">
        <strong>{{ provider.name }}</strong>
        <UiButton type="button" :disabled="busy || !passwordSet || !password || identities.some(item => providerKey(item) === provider.id)" @click="bind(provider.id)">{{ identities.some(item => providerKey(item) === provider.id) ? '已绑定' : '绑定账号' }}</UiButton>
      </div>
    </section>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import FormField from '../../components/FormField.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import { bindExternalIdentity, fetchIdentityPasswordStatus, setupIdentityPassword, fetchExternalIdentities, fetchIdentityProviders, navigateToIdentityProvider, unlinkExternalIdentity, type ExternalIdentity, type IdentityProvider } from '../../api/identities'
const route = useRoute()
const providers = ref<IdentityProvider[]>([]), identities = ref<ExternalIdentity[]>([])
const passwordSet = ref(true), newPassword = ref('')
const password = ref(''), busy = ref(false), message = ref(''), error = ref('')
const providerKey = (identity: ExternalIdentity) => identity.plugin_id
const pluginBaseID = (identity: ExternalIdentity) => identity.plugin_id.split('~', 1)[0]
const providerName = (identity: ExternalIdentity) => providers.value.find(item => item.id === providerKey(identity))?.name || identity.provider_id || pluginBaseID(identity)
async function load() { const [available, linked, security] = await Promise.all([fetchIdentityProviders(), fetchExternalIdentities(), fetchIdentityPasswordStatus()]); providers.value = available; identities.value = linked; passwordSet.value = security.password_set }
onMounted(async () => { try { await load(); if (route.query.linked === '1') message.value = '第三方账号已绑定。下次可从登录页直接登录。' } catch { error.value = '账户安全信息加载失败，请刷新重试。' } })
async function setPassword() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { await setupIdentityPassword(newPassword.value); passwordSet.value = true; message.value = '本地密码已设置，可使用邮箱密码登录。' }
  catch { error.value = '设置失败，请确认密码为 12–72 个 UTF-8 字节；授权过期时请重新通过第三方登录。' }
  finally { newPassword.value = ''; busy.value = false }
}
async function bind(id: string) {
  if (busy.value || !password.value) return
  busy.value = true; message.value = ''; error.value = ''
  try { navigateToIdentityProvider((await bindExternalIdentity(id, password.value)).authorization_url) }
  catch { error.value = '绑定未开始，请检查当前密码和提供方状态。'; busy.value = false }
  finally { password.value = '' }
}
async function unlink(id: string) {
  if (busy.value || !password.value) return
  busy.value = true; message.value = ''; error.value = ''
  try { await unlinkExternalIdentity(id, password.value); await load(); message.value = '第三方账号已解绑。邮箱密码登录仍可使用。' }
  catch { error.value = '解绑失败，请检查密码并刷新重试。' }
  finally { password.value = ''; busy.value = false }
}
</script>
<style scoped>
.panel{padding:24px}.identity-row{display:flex;justify-content:space-between;align-items:flex-start;gap:24px;border-top:1px solid var(--line);padding:18px 0}.identity-summary{min-width:0;flex:1}.identity-details{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px 24px;margin:12px 0 0}.identity-details div{min-width:0}.identity-details dt{color:var(--muted);font-size:12px}.identity-details dd{margin:3px 0 0;overflow-wrap:anywhere;font-size:13px}.mono{font-family:var(--font-mono,monospace)}
@media(max-width:720px){.identity-row{flex-direction:column}.identity-details{grid-template-columns:1fr}.identity-row>button{width:100%}}
</style>
