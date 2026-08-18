<template>
  <div class="setup-shell">
    <aside class="setup-aside">
      <div class="auth-brand"><span class="brand-mark">Z</span><span>zboard</span></div>
      <div>
        <p class="page-eyebrow">首次安装</p>
        <h1>四步完成<br />运营控制台初始化</h1>
        <p>部署密钥保留在服务器环境中；浏览器用于确认站点信息、系统时区与历史保留策略，并创建首位管理员。</p>
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
          <span>步骤 {{ step }} / 4</span>
          <div class="progress"><i :style="{ width: `${step / 4 * 100}%` }"></i></div>
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

        <form v-else-if="step === 2" id="setup-site-form" ref="formElement" class="setup-content" novalidate @submit.prevent="next">
          <p class="page-eyebrow">站点信息</p>
          <h2>配置你的控制台</h2>
          <p class="muted">这些信息可以在系统设置中随时修改。</p>
          <PageAlert v-if="fieldErrors.formError.value" tone="danger" title="站点信息未完成">{{ fieldErrors.formError.value }}</PageAlert>
          <FormField v-slot="{ controlAttrs }" label="站点名称" name="setup-site-name" :error="fieldErrors.fields.site_name" required full><UiInput v-model.trim="form.site_name" v-bind="controlAttrs" maxlength="80" placeholder="我的 zboard" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="公开访问地址" name="setup-site-url" hint="订阅链接和页面跳转将以此地址为基础。" :error="fieldErrors.fields.site_url" required full><UiInput v-model.trim="form.site_url" v-bind="controlAttrs" type="url" placeholder="https://panel.example.com" /></FormField>
          <label class="check-field"><UiCheckbox v-model="form.allow_registration" /><span><strong>允许访客注册</strong><br /><small class="field-hint">关闭后，仅管理员可以创建用户。</small></span></label>
        </form>

        <form v-else-if="step === 3" id="setup-system-form" ref="formElement" class="setup-content" novalidate @submit.prevent="next">
          <p class="page-eyebrow">系统策略</p>
          <h2>确认时间与历史保留策略</h2>
          <p class="muted">时区会影响历史时间展示和业务自然日读取；保留策略控制已结束的运行历史自动清理。安装后仍可在系统设置中修改。</p>
          <PageAlert v-if="fieldErrors.formError.value" tone="danger" title="系统策略未完成">{{ fieldErrors.formError.value }}</PageAlert>
          <FormField v-slot="{ controlAttrs }" label="系统时区" name="setup-system-timezone" hint="使用 IANA 时区，例如 Asia/Shanghai、UTC、America/Los_Angeles。已根据当前浏览器预填。" :error="fieldErrors.fields.system_timezone" required full>
            <UiInput v-model.trim="form.system_timezone" v-bind="controlAttrs" maxlength="100" placeholder="Asia/Shanghai" autocomplete="off" />
          </FormField>
          <div class="retention-grid">
            <FormField v-slot="{ controlAttrs }" label="审计日志保留" name="setup-audit-retention" hint="默认 180 天；0 表示永久保留。" :error="fieldErrors.fields.audit_log_retention_days" required>
              <UiInput v-model.number="form.audit_log_retention_days" v-bind="controlAttrs" type="number" min="0" max="3650" step="1" inputmode="numeric" />
            </FormField>
            <FormField v-slot="{ controlAttrs }" label="运行历史保留" name="setup-operation-retention" hint="默认 90 天；仅清理已结束的节点、发布、证书与供应商操作。" :error="fieldErrors.fields.operation_history_retention_days" required>
              <UiInput v-model.number="form.operation_history_retention_days" v-bind="controlAttrs" type="number" min="0" max="3650" step="1" inputmode="numeric" />
            </FormField>
            <FormField v-slot="{ controlAttrs }" label="运营任务保留" name="setup-task-retention" hint="默认 90 天；仅清理已完成任务及任务项。" :error="fieldErrors.fields.task_history_retention_days" required>
              <UiInput v-model.number="form.task_history_retention_days" v-bind="controlAttrs" type="number" min="0" max="3650" step="1" inputmode="numeric" />
            </FormField>
          </div>
          <div class="policy-note"><UiIcon name="info" /><span>这些设置不会改写数据库中的历史绝对时间；时间戳仍以 UTC 保存，并按照系统时区呈现和解释自然日。</span></div>
        </form>

        <form v-else id="setup-admin-form" ref="formElement" class="setup-content" novalidate @submit.prevent="submit">
          <p class="page-eyebrow">首位用户</p>
          <h2>创建用户并授予管理员权限</h2>
          <p class="muted">这个用户拥有完整的用户中心，同时可以进入后台管理用户、商品、节点、任务与系统配置。</p>
          <PageAlert v-if="fieldErrors.formError.value" tone="danger" title="管理员账户未完成">{{ fieldErrors.formError.value }}</PageAlert>
          <FormField v-slot="{ controlAttrs }" label="用户邮箱" name="setup-admin-email" :error="fieldErrors.fields.admin_email" required full><UiInput v-model.trim="form.admin_email" v-bind="controlAttrs" maxlength="128" type="email" autocomplete="email" placeholder="admin@example.com" /></FormField>
          <div class="form-grid">
            <FormField v-slot="{ controlAttrs }" label="密码" name="setup-admin-password" hint="长度为 12–72 个 UTF-8 字节。" :error="fieldErrors.fields.admin_password" required><UiInput v-model="form.admin_password" v-bind="controlAttrs" minlength="12" maxlength="72" type="password" autocomplete="new-password" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="确认密码" name="setup-confirm-password" hint="再次输入相同密码。" :error="fieldErrors.fields.confirm_password" required><UiInput v-model="confirmPassword" v-bind="controlAttrs" minlength="12" maxlength="72" type="password" autocomplete="new-password" /></FormField>
          </div>
        </form>

        <footer>
          <UiButton v-if="step > 1" variant="secondary" type="button" :disabled="submitting" @click="back">返回</UiButton>
          <UiButton v-if="step === 1" type="button" @click="next">继续</UiButton>
          <UiButton v-else-if="step === 2" form="setup-site-form" type="submit">继续</UiButton>
          <UiButton v-else-if="step === 3" form="setup-system-form" type="submit">继续</UiButton>
          <UiButton v-else form="setup-admin-form" type="submit" :loading="submitting">完成安装</UiButton>
        </footer>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { installZboard } from '../api/client'
import FormField from '../components/FormField.vue'
import PageAlert from '../components/PageAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useAppStore } from '../stores/app'
import { collectFieldErrors, isEmail, isHttpUrl, isUtf8LengthInRange } from '../utils/validation'

const app = useAppStore()
const router = useRouter()
const step = ref(1)
const submitting = ref(false)
const confirmPassword = ref('')
const formElement = ref<HTMLElement | null>(null)
const browserTimezone = (() => {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' }
  catch { return 'UTC' }
})()
const titles = ['检查环境', '配置站点', '系统策略', '创建首位用户']
const descriptions = ['确认服务与数据库连接', '设置站点名称和访问地址', '确认时区和历史保留策略', '为首位用户授予管理员权限']
const form = reactive({
  site_name: 'zboard',
  site_url: window.location.origin,
  allow_registration: true,
  system_timezone: browserTimezone,
  audit_log_retention_days: 180,
  operation_history_retention_days: 90,
  task_history_retention_days: 90,
  admin_email: '',
  admin_password: '',
})
const fieldErrors = useFormErrors()
const formState = useDirtyForm(() => ({ ...form, confirm_password: confirmPassword.value }))
useUnsavedChangesGuard(
  () => formState.dirty.value,
  () => formState.confirmDiscard({
    title: '离开安装向导？',
    message: '尚未完成的站点、系统策略与管理员信息不会被保存。',
    confirmText: '离开向导',
  }),
)

watch(() => form.site_name, () => fieldErrors.clear('site_name'))
watch(() => form.site_url, () => fieldErrors.clear('site_url'))
watch(() => form.system_timezone, () => fieldErrors.clear('system_timezone'))
watch(() => form.audit_log_retention_days, () => fieldErrors.clear('audit_log_retention_days'))
watch(() => form.operation_history_retention_days, () => fieldErrors.clear('operation_history_retention_days'))
watch(() => form.task_history_retention_days, () => fieldErrors.clear('task_history_retention_days'))
watch(() => form.admin_email, () => fieldErrors.clear('admin_email'))
watch(() => form.admin_password, () => fieldErrors.clear('admin_password'))
watch(confirmPassword, () => fieldErrors.clear('confirm_password'))

function back() {
  fieldErrors.clear()
  step.value = Math.max(1, step.value - 1)
}

function isValidIANATimezone(value: string) {
  if (!value.trim()) return false
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: value.trim() }).format()
    return true
  } catch {
    return false
  }
}

function retentionError(value: number) {
  return (!Number.isInteger(value) || value < 0 || value > 3650) && '请输入 0–3650 的整数；0 表示永久保留。'
}

async function next() {
  if (step.value === 2) {
    form.site_name = form.site_name.trim()
    form.site_url = form.site_url.trim().replace(/\/+$/, '')
    const valid = await fieldErrors.applyValidation(collectFieldErrors({
      site_name: !isUtf8LengthInRange(form.site_name, 1, 80, true) && '站点名称必须为 1–80 个 UTF-8 字节。',
      site_url: !isHttpUrl(form.site_url) && '请输入不含账号、密码或片段的完整 HTTP 或 HTTPS 地址。',
    }), formElement, '请更正标记字段后再继续。')
    if (!valid) return
  }
  if (step.value === 3) {
    form.system_timezone = form.system_timezone.trim()
    const valid = await fieldErrors.applyValidation(collectFieldErrors({
      system_timezone: !isValidIANATimezone(form.system_timezone) && '请输入有效的 IANA 时区，例如 Asia/Shanghai。',
      audit_log_retention_days: retentionError(form.audit_log_retention_days),
      operation_history_retention_days: retentionError(form.operation_history_retention_days),
      task_history_retention_days: retentionError(form.task_history_retention_days),
    }), formElement, '请更正标记字段后再继续。')
    if (!valid) return
  }
  step.value = Math.min(4, step.value + 1)
}

async function submit() {
  form.admin_email = form.admin_email.trim().toLowerCase()
  const valid = await fieldErrors.applyValidation(collectFieldErrors({
    admin_email: !isEmail(form.admin_email) && '请输入不超过 128 个 UTF-8 字节的有效管理员邮箱。',
    admin_password: !isUtf8LengthInRange(form.admin_password, 12, 72) && '管理员密码必须为 12–72 个 UTF-8 字节。',
    confirm_password: form.admin_password !== confirmPassword.value && '两次输入的密码不一致。',
  }), formElement, '请更正标记字段后再完成安装。')
  if (!valid) return
  submitting.value = true
  try {
    const result = await installZboard(form)
    app.completeSetup(result)
    formState.markClean()
    await router.replace('/admin/dashboard')
  } catch (e: any) {
    await fieldErrors.applyApiError(e, '安装失败，请检查服务日志后重试。', formElement, {
      site_name: 'site_name',
      site_url: 'site_url',
      system_timezone: 'system_timezone',
      audit_log_retention_days: 'audit_log_retention_days',
      operation_history_retention_days: 'operation_history_retention_days',
      task_history_retention_days: 'task_history_retention_days',
      admin_email: 'admin_email',
      admin_password: 'admin_password',
    })
    if (e?.response?.status === 409) { formState.markClean(); await app.loadSetupStatus(true); await router.replace('/login') }
  } finally { submitting.value = false }
}

</script>

<style scoped>
.setup-shell { min-height: 100vh; display: grid; grid-template-columns: minmax(300px, 38%) minmax(0, 1fr); background: var(--page); }
.setup-aside { position: relative; display: flex; flex-direction: column; justify-content: space-between; gap: 40px; overflow: hidden; padding: 46px; color: var(--sidebar-text); background: var(--sidebar); }
.setup-aside::before { content: ''; position: absolute; inset: 0; opacity: .25; background-image: linear-gradient(var(--auth-grid-line) 1px, transparent 1px), linear-gradient(90deg, var(--auth-grid-line) 1px, transparent 1px); background-size: 44px 44px; mask-image: linear-gradient(to bottom right, var(--color-black), transparent 76%); pointer-events: none; }.setup-aside > * { position: relative; z-index: 1; }
.setup-aside h1 { margin: 0 0 14px; color: var(--text-inverse); font-size: clamp(32px, 4vw, 48px); line-height: 1.12; }.setup-aside > div > p:last-child { max-width: 500px; color: var(--setup-muted-soft); line-height: 1.7; }
.setup-steps { display: grid; gap: 18px; margin: 0; padding: 0; list-style: none; }.setup-steps li { display: grid; grid-template-columns: 34px 1fr; gap: 12px; opacity: .48; }.setup-steps li > span { width: 30px; height: 30px; display: grid; place-items: center; border: 1px solid var(--setup-muted-strong); border-radius: 50%; font-size: 12px; }.setup-steps li div { display: grid; gap: 2px; }.setup-steps strong { color: var(--info-soft); font-size: 13px; }.setup-steps small { color: var(--setup-muted); }.setup-steps li.active, .setup-steps li.complete { opacity: 1; }.setup-steps li.active > span { border-color: var(--focus-border); background: var(--primary); color: var(--text-inverse); }.setup-steps li.complete > span { border-color: var(--success-accent); color: var(--success-accent-soft); }
.setup-main { display: grid; place-items: center; padding: 40px; }.setup-card { width: min(760px, 100%); overflow: hidden; background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-lg); box-shadow: 0 24px 70px var(--card-shadow); }.setup-card > header { padding: 18px 30px 0; }.setup-card > header > span { color: var(--muted); font-size: 11px; font-weight: 700; }.progress { height: 4px; margin-top: 12px; overflow: hidden; border-radius: 999px; background: var(--surface-muted); }.progress i { display: block; height: 100%; background: var(--primary); transition: width .2s ease; }
.setup-content { display: grid; gap: 18px; padding: 28px 30px; }.setup-content h2 { margin: -8px 0 0; font-size: 25px; }.setup-content > .muted { margin: -8px 0 4px; line-height: 1.6; }.check-list { display: grid; border: 1px solid var(--line); border-radius: 12px; }.check-list > div { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 14px; }.check-list > div + div { border-top: 1px solid var(--line); }.check-icon { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 9px; color: var(--primary); background: var(--primary-soft); }.check-list div > div { min-width: 0; display: grid; gap: 2px; }.check-list strong { font-size: 13px; }.check-list small { overflow: hidden; color: var(--muted); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.retention-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }.policy-note { display: flex; align-items: flex-start; gap: 9px; padding: 12px 14px; border: 1px solid var(--line); border-radius: 10px; color: var(--muted); background: var(--surface-soft); font-size: 11px; line-height: 1.6; }.policy-note :deep(svg) { flex: 0 0 auto; margin-top: 2px; color: var(--primary); }
.setup-card footer { display: flex; justify-content: flex-end; gap: 9px; padding: 17px 30px; border-top: 1px solid var(--line); background: var(--surface-soft); }
@media (max-width: 820px) { .setup-shell { grid-template-columns: 1fr; }.setup-aside { gap: 22px; padding: 28px; }.setup-steps { grid-template-columns: repeat(4, 1fr); gap: 8px; }.setup-steps li { grid-template-columns: 1fr; }.setup-steps small { display: none; }.setup-main { padding: 22px 14px; }.retention-grid { grid-template-columns: 1fr; } }
@media (max-width: 520px) { .setup-aside h1 { font-size: 28px; }.setup-aside > div > p:last-child { display: none; }.setup-content { padding: 24px 20px; }.setup-card > header { padding-inline: 20px; }.setup-card footer { padding-inline: 20px; } }
</style>
