<template>
  <main class="auth-shell"><section class="auth-card stack">
    <h1>{{ failed ? '第三方登录未完成' : '正在完成身份验证' }}</h1>
    <p :role="failed ? 'alert' : 'status'">{{ statusText }}</p>
    <template v-if="failed"><RouterLink to="/login">返回邮箱登录</RouterLink><RouterLink to="/account/security">前往账户安全绑定</RouterLink></template>
  </section></main>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { finishIdentityLogin } from '../api/identities'
import { useAppStore } from '../stores/app'
const route = useRoute(), router = useRouter(), store = useAppStore()
const failed = ref(false), statusText = ref('正在向 ZBoard 确认登录结果…')
onMounted(async () => {
  try {
    if (route.query.error) throw new Error('provider failure')
    const result = await finishIdentityLogin()
    if (result.linked) { await router.replace({path:'/account/security',query:{linked:'1'}}); return }
    if (!result.auth?.token || !result.user) throw new Error('missing login result')
    store.setToken(result.auth.token); store.setUser(result.user)
    await store.loadSystemStatus(true).catch(() => undefined)
    await router.replace(result.user.is_admin ? '/admin/dashboard' : '/account')
  } catch {
    failed.value = true
    statusText.value = '授权被取消、登录已过期、账号尚未绑定或提供方不可用。首次使用请通过邮箱登录后绑定账号，再重试。'
  }
})
</script>
