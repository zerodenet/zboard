<template>
  <section class="standard-page">
    <PageHeader title="注册与验证" description="集中管理注册入口、身份验证方式和注册成功后的系统通知；后续图形验证码等验证模块也在这里扩展。" eyebrow="Identity">
      <template #actions><PageRefreshButton label="刷新注册设置" :loading="loading" @click="reload" /></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="注册设置已保存" error-title="注册设置操作失败" />

    <UiMetricStrip>
      <MetricCard label="公开注册" :value="registrationEnabled ? '开放' : '关闭'" icon="users" :tone="registrationEnabled ? 'success' : 'neutral'" status="入口控制" meta="决定访客能否创建账户" />
      <MetricCard label="邮箱验证码" :value="verificationEnabled ? '必需' : '未启用'" icon="audit" :tone="verificationEnabled ? 'info' : 'neutral'" status="身份验证" :meta="verificationEnabled ? '注册前发送六位验证码' : '保留原直接注册流程'" />
      <MetricCard label="SMTP 通道" :value="smtpReady ? '可用' : '未就绪'" icon="activity" :tone="smtpReady ? 'success' : 'warning'" status="投递依赖" :meta="smtpReady ? '可发送注册验证码' : '请先完成邮件通道配置'" />
    </UiMetricStrip>

    <div class="registration-grid">
      <UiSection title="注册入口" description="这是注册功能的总开关；关闭后公开注册页不再接受新账户。">
        <div class="setting-control">
          <div><strong>允许访客注册</strong><p>管理员创建用户、已有用户登录和账户数据不受影响。</p></div>
          <UiCheckbox v-model="registrationEnabled" role="switch" aria-label="允许访客注册" />
        </div>
        <div class="section-actions"><UiButton type="button" :loading="savingRegistration" :disabled="!registrationDirty" @click="saveRegistration">保存注册入口</UiButton></div>
      </UiSection>

      <UiSection title="邮箱验证码" description="启用后，注册表单要求先向目标邮箱发送并填写六位验证码。">
        <div class="setting-control">
          <div><strong>注册时验证邮箱</strong><p>验证码十分钟有效，并包含重发冷却、尝试次数和请求网络频率限制。</p></div>
          <UiCheckbox v-model="verificationEnabled" role="switch" aria-label="注册时验证邮箱" />
        </div>
        <PageAlert v-if="verificationEnabled && !smtpReady" tone="warning" title="SMTP 通道尚未就绪">请先在“邮件与运营模板”完成 SMTP 主机、发件人、TLS 和凭证配置，再启用邮箱验证。</PageAlert>
        <div class="section-actions"><RouterLink class="settings-link" to="/admin/settings/email">配置邮件通道</RouterLink><UiButton type="button" :loading="savingVerification" :disabled="!verificationDirty || (verificationEnabled && !smtpReady)" @click="saveVerification">保存邮箱验证</UiButton></div>
      </UiSection>

      <UiSection title="页面图形验证码" description="为后续人机验证保留的独立模块，不与邮箱验证码或 SMTP 配置混在一起。">
        <div class="setting-control disabled-module">
          <div><strong>图形验证码提供方</strong><p>当前版本尚未接入；后续可在此配置提供方、站点密钥、触发场景和失败策略。</p></div>
          <StatusBadge tone="neutral">尚未接入</StatusBadge>
        </div>
      </UiSection>
    </div>

    <UiSection title="注册流程与通知" description="验证码负责证明邮箱可用；欢迎通知是注册成功后进入持久化任务队列的另一条链路。">
      <div class="registration-flow" aria-label="注册处理流程">
        <div><span>1</span><strong>开放注册入口</strong><small>决定访客是否可以提交注册</small></div>
        <UiIcon name="chevron" />
        <div><span>2</span><strong>完成身份验证</strong><small>{{ verificationEnabled ? '邮箱验证码验证通过' : '当前不要求额外验证' }}</small></div>
        <UiIcon name="chevron" />
        <div><span>3</span><strong>创建用户</strong><small>账号事实写入用户资源</small></div>
        <UiIcon name="chevron" />
        <div><span>4</span><strong>排队欢迎通知</strong><small>仅在下方模板启用时创建邮件任务</small></div>
      </div>
      <PageAlert tone="info" title="验证码邮件与欢迎通知不是同一件事">验证码在注册前同步发送；欢迎通知在注册成功后创建可重试任务，可在“运营任务”查看数量、进度和结果。</PageAlert>
    </UiSection>

    <UiSection title="注册成功通知" description="这是系统注册触发器的唯一模板。启用后每个成功注册会创建一条欢迎邮件任务；停用不影响验证码。">
      <EmailTemplateManager mode="registration" @dirty="templateDirty = $event" />
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchSystemConfigs, updateSiteSettings, updateSystemConfig, type SystemConfig } from '../api/client'
import EmailTemplateManager from '../components/EmailTemplateManager.vue'
import MetricCard from '../components/MetricCard.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import UiIcon from '../components/UiIcon.vue'
import UiMetricStrip from '../components/UiMetricStrip.vue'
import UiSection from '../components/UiSection.vue'
import { useUnsavedChangesGuard } from '../composables/useFormState'
import { useAppStore } from '../stores/app'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { confirmAction } from '../utils/feedback'

const app = useAppStore()
const loading = ref(false)
const savingRegistration = ref(false)
const savingVerification = ref(false)
const registrationEnabled = ref(false)
const initialRegistrationEnabled = ref(false)
const verificationEnabled = ref(false)
const initialVerificationEnabled = ref(false)
const configs = ref<SystemConfig[]>([])
const templateDirty = ref(false)
const message = ref('')
const error = ref('')

const registrationConfig = computed(() => configs.value.find(item => item.config_key === 'register_email_verification'))
const registrationDirty = computed(() => registrationEnabled.value !== initialRegistrationEnabled.value)
const verificationDirty = computed(() => verificationEnabled.value !== initialVerificationEnabled.value)
const smtpReady = computed(() => {
  const byKey = new Map(configs.value.map(item => [item.config_key, item]))
  const username = String(byKey.get('smtp_username')?.value || '').trim()
  const passwordReady = !username || Boolean(byKey.get('smtp_password')?.configured)
  return Boolean(
    String(byKey.get('smtp_host')?.value || '').trim()
    && Number(byKey.get('smtp_port')?.value || 0) > 0
    && String(byKey.get('smtp_from')?.value || '').trim()
    && String(byKey.get('smtp_tls_mode')?.value || '').trim()
    && passwordReady,
  )
})

useUnsavedChangesGuard(
  () => registrationDirty.value || verificationDirty.value || templateDirty.value,
  () => confirmAction({ title: '离开注册与验证？', message: '当前仍有注册设置或通知模板尚未保存。', confirmText: '放弃修改', tone: 'danger' }),
)

async function load() {
  loading.value = true; error.value = ''
  try {
    const [setup, nextConfigs] = await Promise.all([app.loadSetupStatus(true), fetchSystemConfigs()])
    configs.value = nextConfigs
    registrationEnabled.value = Boolean(setup?.allow_registration)
    initialRegistrationEnabled.value = registrationEnabled.value
    const verification = nextConfigs.find(item => item.config_key === 'register_email_verification')
    verificationEnabled.value = verification?.value === true
    initialVerificationEnabled.value = verificationEnabled.value
  } catch (cause: any) { error.value = normalizeApiErrorMessage(cause, '注册设置加载失败。') }
  finally { loading.value = false }
}

async function reload() {
  if ((registrationDirty.value || verificationDirty.value) && !await confirmAction({ title: '重新载入注册设置？', message: '未保存的开关修改将被服务器当前值替换。', confirmText: '重新载入', tone: 'danger' })) return
  await load()
}

async function saveRegistration() {
  const setup = app.installation
  if (!setup) { error.value = '站点安装信息尚未加载。'; return }
  savingRegistration.value = true; message.value = ''; error.value = ''
  try {
    await updateSiteSettings({ site_name: setup.site_name || app.siteName || 'zboard', site_url: setup.site_url || window.location.origin, allow_registration: registrationEnabled.value })
    await app.loadSetupStatus(true)
    initialRegistrationEnabled.value = registrationEnabled.value
    message.value = registrationEnabled.value ? '公开注册入口已开放。' : '公开注册入口已关闭。'
  } catch (cause: any) { error.value = normalizeApiErrorMessage(cause, '注册入口保存失败。') }
  finally { savingRegistration.value = false }
}

async function saveVerification() {
  const config = registrationConfig.value
  if (!config) { error.value = '系统未返回注册邮箱验证码配置。'; return }
  savingVerification.value = true; message.value = ''; error.value = ''
  try {
    const updated = await updateSystemConfig(config.config_key, verificationEnabled.value, config.revision)
    const index = configs.value.findIndex(item => item.config_key === updated.config_key)
    if (index >= 0) configs.value[index] = updated
    initialVerificationEnabled.value = verificationEnabled.value
    await app.loadPublicConfigs()
    message.value = verificationEnabled.value ? '注册邮箱验证码已启用。' : '注册邮箱验证码已停用。'
  } catch (cause: any) { error.value = normalizeApiErrorMessage(cause, '邮箱验证设置保存失败。') }
  finally { savingVerification.value = false }
}

onMounted(load)
</script>

<style scoped>
.registration-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.registration-grid>:last-child{grid-column:1/-1}.setting-control{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:16px}.setting-control strong{font-size:13px}.setting-control p{margin:4px 0 0;color:var(--muted);font-size:10px;line-height:1.55}.disabled-module{opacity:.82}.section-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px;padding:0 16px 16px}.settings-link{margin-right:auto;color:var(--primary);font-size:11px;font-weight:700;text-decoration:none}.registration-flow{display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr) auto minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:10px;padding:16px}.registration-flow>div{min-height:96px;display:grid;align-content:start;gap:5px;padding:13px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}.registration-flow>div span{width:24px;height:24px;display:grid;place-items:center;border-radius:999px;color:var(--primary);background:var(--primary-soft);font-size:10px;font-weight:800}.registration-flow strong{font-size:11px}.registration-flow small{color:var(--muted);font-size:9px;line-height:1.45}.registration-flow>.ui-icon{color:var(--muted)}@media(max-width:900px){.registration-grid{grid-template-columns:1fr}.registration-grid>:last-child{grid-column:auto}.registration-flow{grid-template-columns:1fr}.registration-flow>.ui-icon{justify-self:center;transform:rotate(90deg)}}
</style>
