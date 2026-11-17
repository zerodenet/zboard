<template>
  <section v-if="methods.length || state.error || !state.loaded" class="plugin-login-methods" :class="{'plugin-login-methods--cold': coldStart && methods.length > 0 || coldStart && !state.loaded}" aria-label="其他登录方式">
    <div v-if="!state.loaded && !state.error" class="login-method-placeholder" role="status" aria-label="正在读取登录方式"><span /></div>
    <div v-for="method in methods" :key="`${method.contribution.plugin_id}:${method.provider.id}`" class="login-method">
      <UiButton type="button" variant="secondary" class="login-method-button" :loading="busy === method.provider.id" :disabled="Boolean(busy) || state.loading || method.contribution.availability === 'unavailable'" @click="start(method.contribution, method.provider.id)"><span class="login-method-name">{{ loginLabel(method.provider.name) }}</span></UiButton>
    </div>
    <div v-if="state.error || actionError || unavailable" class="login-method-status" role="status"><span>{{ actionError || (state.error ? '其他登录方式暂不可用' : methods.some(item => item.contribution.availability !== 'unavailable') ? '部分登录方式暂不可用' : '登录服务暂不可用') }}</span><UiButton variant="ghost" size="sm" :disabled="state.loading || Boolean(busy)" @click="retry">重试</UiButton></div>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { getAuthToken } from '../api/client'
import { useAppStore } from '../stores/app'
import UiButton from '../components/UiButton.vue'
import { compositionState, preparePluginComposition } from './composition'
import { startPluginIdentity, type CatalogSlot } from '../api/plugins'
const props = defineProps<{ beforeIdentityLogin?: () => boolean }>()
const emit = defineEmits<{ state: [value: {count: number; loading: boolean; error: string}] }>()
const state = compositionState('public').state
const app = useAppStore()
const coldStart = !state.loaded
let mounted = true, navigating = false
const methods = computed(() => state.slots.filter(item => item.slot.slot === 'auth.login.methods').flatMap(contribution => (contribution.providers?.length ? contribution.providers : [{ id: contribution.plugin_id, name: contribution.slot.title }]).map(provider => ({ contribution, provider }))))
const unavailable = computed(() => methods.value.some(item => item.contribution.availability === 'unavailable'))
const busy = ref(''), actionError = ref('')
function loginLabel(name: string) { return /登录|login/i.test(name) ? name : `${name} 登录` }
async function retry() { actionError.value = ''; await preparePluginComposition('public',true) }
async function start(contribution: CatalogSlot, provider: string) {
  if (busy.value || contribution.availability === 'unavailable') return
  busy.value = provider; actionError.value = ''
  const actor = `${getAuthToken()}:${app.token}:${app.user?.id}:${app.sessionEpoch}`
  try {
    const result = await startPluginIdentity(contribution.plugin_id, contribution.slot.id, provider, contribution.generation, contribution.config_revision || 0)
    if (!mounted) return
    if (actor !== `${getAuthToken()}:${app.token}:${app.user?.id}:${app.sessionEpoch}`) throw new Error('identity changed')
    const current = state.slots.find(item => item.plugin_id === contribution.plugin_id && item.slot.id === contribution.slot.id)
    if (!current || current.generation !== contribution.generation || current.config_revision !== contribution.config_revision || current.availability === 'unavailable') throw new Error('composition changed')
    const url = new URL(result.authorization_url)
    const local = url.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(url.hostname)
    if (url.username || url.password || url.protocol !== 'https:' && !local) throw new Error('unsafe navigation')
    if (props.beforeIdentityLogin && !props.beforeIdentityLogin()) return
    navigating = true
    window.location.assign(url.href)
  } catch (cause: any) { navigating = false; if (mounted) actionError.value = loginFailure(cause) }
  finally { if (!navigating) busy.value = '' }
}
watch(() => [methods.value.length,state.loading,state.error], () => emit('state',{count:methods.value.length,loading:state.loading || !state.loaded && !state.error,error:state.error}), {immediate:true})
function loginFailure(cause: any) {
  const message = cause?.response?.data?.message
  if (message === 'invalid authentication origin or content type') return '站点地址与登录配置不一致，请联系管理员。'
  if (message === 'identity provider is unavailable; review its configuration') return '登录服务暂不可用，请联系管理员检查配置。'
  if (cause?.response?.status === 409) return '登录方式配置已更新，请刷新后重试。'
  if (cause?.response?.status === 403) return '此登录方式已停用或不可访问，请刷新后重试。'
  if (cause?.response?.status === 503) return '登录服务暂不可用，请稍后重试。'
  return message || '登录暂未完成，请重试。'
}
function returned() { navigating = false; busy.value = '' }
onMounted(() => { void preparePluginComposition('public'); window.addEventListener('pageshow',returned) })
onBeforeUnmount(() => { mounted = false; window.removeEventListener('pageshow',returned) })
</script>
<style scoped>
.plugin-login-methods { display: grid; gap: 8px; min-width: 0; }
.plugin-login-methods--cold { height: 128px; overflow: auto; align-content: start; }
.login-method-button { width: 100%; justify-content: center; }
.login-method-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.login-method-placeholder { height: 36px; border-radius: 6px; background: var(--surface-neutral); }
.login-method-status { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 12px; color: var(--muted-foreground); }
</style>
