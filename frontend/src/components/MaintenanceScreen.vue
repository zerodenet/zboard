<template>
  <main class="maintenance-screen">
    <section class="maintenance-card" role="status" aria-live="polite">
      <span class="maintenance-mark">Z</span>
      <p class="maintenance-eyebrow">Service maintenance</p>
      <h1>{{ title }}</h1>
      <p class="maintenance-message">{{ message }}</p>
      <div v-if="migrationInProgress" class="maintenance-progress"><i></i><span>正在执行数据库迁移与一致性校验</span></div>
      <small>管理员仍可登录运营控制台处理维护任务。</small>
      <button v-if="hasToken" class="maintenance-login" type="button" @click="switchAccount">切换到管理员账户</button>
      <RouterLink v-else to="/login">管理员登录</RouterLink>
    </section>
  </main>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'
defineProps<{ title: string; message: string; migrationInProgress: boolean; hasToken: boolean }>()
const app = useAppStore()
const router = useRouter()
function switchAccount() { app.clear(); void router.push('/login') }
</script>

<style scoped>
.maintenance-screen { min-height: 100vh; display: grid; place-items: center; padding: 28px; background: radial-gradient(circle at 20% 10%, var(--primary-soft), transparent 36%), var(--page); }
.maintenance-card { width: min(620px, 100%); padding: clamp(32px, 7vw, 64px); border: 1px solid var(--line); border-radius: 24px; background: var(--surface); box-shadow: var(--shadow-md); text-align: center; }
.maintenance-mark { display: inline-grid; place-items: center; width: 54px; height: 54px; border-radius: 16px; background: var(--primary); color: white; font-size: 26px; font-weight: 850; }
.maintenance-eyebrow { margin: 22px 0 8px; color: var(--primary); font-size: 11px; font-weight: 800; letter-spacing: .14em; text-transform: uppercase; }
h1 { margin: 0; font-size: clamp(30px, 6vw, 48px); }
.maintenance-message { margin: 18px auto 24px; max-width: 480px; color: var(--muted); line-height: 1.8; white-space: pre-wrap; }
.maintenance-progress { display: flex; align-items: center; justify-content: center; gap: 10px; margin: 18px 0; color: var(--primary); font-weight: 700; }
.maintenance-progress i { width: 10px; height: 10px; border-radius: 50%; background: currentColor; animation: pulse 1.2s ease-in-out infinite; }
small { display: block; color: var(--muted); }
a, .maintenance-login { display: inline-flex; margin-top: 22px; color: var(--primary); font-weight: 750; }
.maintenance-login { margin-inline: auto; border: 0; background: transparent; text-decoration: underline; cursor: pointer; }
@keyframes pulse { 50% { opacity: .3; transform: scale(.75); } }
</style>
