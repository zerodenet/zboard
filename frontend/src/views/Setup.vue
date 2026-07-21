<template>
  <div class="setup-shell">
    <aside class="setup-aside">
      <div class="auth-brand"><span class="brand-mark">Z</span><span>zboard</span></div>
      <div>
        <p class="page-eyebrow">首次安装</p>
        <h1>三步完成<br />运营控制台初始化</h1>
        <p>部署密钥保留在服务器环境中；浏览器只配置站点信息，并为首位用户授予管理员权限。</p>
      </div>
      <ol class="setup-steps">
        <li v-for="(title, index) in titles" :key="title" :class="{ active: step === index + 1, complete: step > index + 1 }">
          <span>{{ step > index + 1 ? '✓' : index + 1 }}</span>
          <div><strong>{{ title }}</strong><small>{{ descriptions[index] }}</small></div>
        </li>
      </ol>
    </aside>

    <main class="setup-main">
      <section class="setup-card">
        <header>
          <span>步骤 {{ step }} / 3</span>
          <div class="progress"><i :style="{ width: `${step / 3 * 100}%` }"></i></div>
        </header>

        <div v-if="step === 1" class="setup-content">
          <p class="page-eyebrow">环境检查</p>
          <h2>服务已经准备就绪</h2>
          <p class="muted">确认 API、数据库连接及当前构建版本，然后开始配置站点。</p>
          <div class="check-list">
            <div><span class="check-icon"><UiIcon name="check" /></span><div><strong>HTTP API</strong><small>应用接口可以正常响应</small></div><StatusBadge tone="success">已连接</StatusBadge></div>
            <div><span class="check-icon"><UiIcon name="database" /></span><div><strong>MySQL 数据库</strong><small>迁移完成并可读写</small></div><StatusBadge tone="success">已连接</StatusBadge></div>
            <div><span class="check-icon"><UiIcon name="shield" /></span><div><strong>当前版本</strong><small class="version-text">{{ app.installation?.version || 'unknown' }}</small></div><StatusBadge tone="info">已加载</StatusBadge></div>
          </div>
          <div class="alert"><UiIcon name="shield" />数据库密码、JWT 与加密密钥始终由部署环境管理，不会发送到浏览器。</div>
        </div>

        <form v-else-if="step === 2" class="setup-content" @submit.prevent="next">
          <p class="page-eyebrow">站点信息</p>
          <h2>配置你的控制台</h2>
          <p class="muted">这些信息可以在系统设置中随时修改。</p>
          <label class="field"><span>站点名称</span><input v-model.trim="form.site_name" maxlength="80" required placeholder="我的 zboard" /></label>
          <label class="field"><span>公开访问地址</span><input v-model.trim="form.site_url" type="url" required placeholder="https://panel.example.com" /><small class="field-hint">订阅链接和页面跳转将以此地址为基础。</small></label>
          <label class="check-field"><input v-model="form.allow_registration" type="checkbox" /><span><strong>允许访客注册</strong><br /><small class="field-hint">关闭后，仅管理员可以创建用户。</small></span></label>
        </form>

        <form v-else class="setup-content" @submit.prevent="submit">
          <p class="page-eyebrow">首位用户</p>
          <h2>创建用户并授予管理员权限</h2>
          <p class="muted">这个用户拥有完整的用户中心，同时可以进入后台管理用户、商品、节点、任务与系统配置。</p>
          <label class="field"><span>用户邮箱</span><input v-model.trim="form.admin_email" maxlength="128" type="email" autocomplete="email" required placeholder="admin@example.com" /></label>
          <div class="form-grid">
            <label class="field"><span>密码</span><input v-model="form.admin_password" minlength="12" maxlength="72" type="password" autocomplete="new-password" required /></label>
            <label class="field"><span>确认密码</span><input v-model="confirmPassword" minlength="12" maxlength="72" type="password" autocomplete="new-password" required /></label>
          </div>
          <small class="field-hint">密码长度为 12–72 个 UTF-8 字节，服务端将只保存安全哈希。</small>
        </form>

        <div v-if="error" class="alert alert-danger"><UiIcon name="alert" />{{ error }}</div>
        <footer>
          <button v-if="step > 1" class="button button-secondary" type="button" :disabled="submitting" @click="step--">返回</button>
          <button v-if="step < 3" class="button" type="button" @click="next">继续</button>
          <button v-else class="button" type="button" :disabled="submitting" @click="submit">{{ submitting ? '正在初始化…' : '完成安装' }}</button>
        </footer>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { installZboard } from '../api/client'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const router = useRouter()
const step = ref(1)
const submitting = ref(false)
const error = ref('')
const confirmPassword = ref('')
const titles = ['检查环境', '配置站点', '创建首位用户']
const descriptions = ['确认服务与数据库连接', '设置站点名称和访问地址', '为首位用户授予管理员权限']
const form = reactive({ site_name: 'zboard', site_url: window.location.origin, allow_registration: true, admin_email: '', admin_password: '' })

function next() {
  error.value = ''
  if (step.value === 2 && (!form.site_name || !/^https?:\/\//.test(form.site_url))) {
    error.value = '请输入站点名称和完整的 HTTP 或 HTTPS 地址。'
    return
  }
  step.value = Math.min(3, step.value + 1)
}

async function submit() {
  error.value = ''
  if (!/^\S+@\S+\.\S+$/.test(form.admin_email)) { error.value = '请输入有效的管理员邮箱。'; return }
  const bytes = new TextEncoder().encode(form.admin_password).length
  if (bytes < 12 || bytes > 72) { error.value = '管理员密码必须为 12–72 个 UTF-8 字节。'; return }
  if (form.admin_password !== confirmPassword.value) { error.value = '两次输入的密码不一致。'; return }
  submitting.value = true
  try {
    const result = await installZboard(form)
    app.completeSetup(result)
    await router.replace('/admin/dashboard')
  } catch (e: any) {
    error.value = e?.response?.data?.message || '安装失败，请检查服务日志后重试。'
    if (e?.response?.status === 409) { await app.loadSetupStatus(true); await router.replace('/login') }
  } finally { submitting.value = false }
}
</script>

<style scoped>
.setup-shell { min-height: 100vh; display: grid; grid-template-columns: minmax(300px, 38%) minmax(0, 1fr); background: #f8fafc; }
.setup-aside { display: flex; flex-direction: column; justify-content: space-between; gap: 40px; padding: 46px; color: #cbd5e1; background: linear-gradient(150deg, #0d1729, #15325a); }
.setup-aside h1 { margin: 0 0 14px; color: #fff; font-size: clamp(32px, 4vw, 48px); line-height: 1.12; }.setup-aside > div > p:last-child { max-width: 500px; color: #9fb0c8; line-height: 1.7; }
.setup-steps { display: grid; gap: 18px; margin: 0; padding: 0; list-style: none; }.setup-steps li { display: grid; grid-template-columns: 34px 1fr; gap: 12px; opacity: .48; }.setup-steps li > span { width: 30px; height: 30px; display: grid; place-items: center; border: 1px solid #7890b2; border-radius: 50%; font-size: 12px; }.setup-steps li div { display: grid; gap: 2px; }.setup-steps strong { color: #eef4ff; font-size: 13px; }.setup-steps small { color: #8da0bb; }.setup-steps li.active, .setup-steps li.complete { opacity: 1; }.setup-steps li.active > span { border-color: #60a5fa; background: #2563eb; color: #fff; }.setup-steps li.complete > span { border-color: #34d399; color: #6ee7b7; }
.setup-main { display: grid; place-items: center; padding: 40px; }.setup-card { width: min(720px, 100%); overflow: hidden; background: #fff; border: 1px solid var(--line); border-radius: 20px; box-shadow: 0 24px 70px rgb(16 24 40 / .1); }.setup-card > header { padding: 18px 30px 0; }.setup-card > header > span { color: var(--muted); font-size: 11px; font-weight: 700; }.progress { height: 4px; margin-top: 12px; overflow: hidden; border-radius: 999px; background: #eef2f6; }.progress i { display: block; height: 100%; background: linear-gradient(90deg, #2563eb, #06b6d4); transition: width .2s ease; }
.setup-content { display: grid; gap: 18px; padding: 28px 30px; }.setup-content h2 { margin: -8px 0 0; font-size: 25px; }.setup-content > .muted { margin: -8px 0 4px; line-height: 1.6; }.check-list { display: grid; border: 1px solid var(--line); border-radius: 12px; }.check-list > div { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 14px; }.check-list > div + div { border-top: 1px solid var(--line); }.check-icon { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 9px; color: var(--primary); background: var(--primary-soft); }.check-list div > div { min-width: 0; display: grid; gap: 2px; }.check-list strong { font-size: 13px; }.check-list small { overflow: hidden; color: var(--muted); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.setup-card footer { display: flex; justify-content: flex-end; gap: 9px; padding: 17px 30px; border-top: 1px solid var(--line); background: var(--surface-soft); }
@media (max-width: 820px) { .setup-shell { grid-template-columns: 1fr; }.setup-aside { gap: 22px; padding: 28px; }.setup-steps { grid-template-columns: repeat(3, 1fr); gap: 8px; }.setup-steps li { grid-template-columns: 1fr; }.setup-steps small { display: none; }.setup-main { padding: 22px 14px; } }
@media (max-width: 520px) { .setup-aside h1 { font-size: 28px; }.setup-aside > div > p:last-child { display: none; }.setup-content { padding: 24px 20px; }.setup-card > header { padding-inline: 20px; }.setup-card footer { padding-inline: 20px; } }
</style>
