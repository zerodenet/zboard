<template>
  <section class="standard-page registration-settings-page">
    <PageHeader title="注册与验证" :show-section-navigation="false">
      <template #actions><PageRefreshButton label="刷新注册设置" :loading="loading" @click="reload" /></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="注册设置已保存" error-title="注册设置操作失败" />

    <PageAlert v-if="registrationDraftRecovery.message.value" tone="info" title="重新认证后的草稿">{{ registrationDraftRecovery.message.value }}<UiButton variant="ghost" type="button" @click="registrationDraftRecovery.restore()">重新检查草稿恢复</UiButton></PageAlert>
    <PageAlert v-if="registrationRecoveryConflict || verificationRecoveryConflict" tone="warning" title="注册设置已变化">原开关草稿已保留；存在冲突的开关不能保存，请刷新当前值后重新编辑。</PageAlert>
    <SettingsPageLayout>
      <div class="registration-grid">
        <UiSection title="注册入口">
          <div class="setting-control">
            <div><strong>允许访客注册</strong><p>管理员创建用户、已有用户登录和账户数据不受影响。</p></div>
            <UiSwitch v-model="registrationEnabled" aria-label="允许访客注册" />
          </div>
          <div class="section-actions"><UiButton type="button" :loading="savingRegistration" :disabled="registrationRecoveryConflict || !registrationDirty" @click="saveRegistration">保存注册入口</UiButton></div>
        </UiSection>

        <UiSection title="邮箱验证码" description="启用后，注册表单要求先向目标邮箱发送并填写六位验证码。">
          <div class="setting-control">
            <div><strong>注册时验证邮箱</strong><p>验证码十分钟有效。</p></div>
            <UiSwitch v-model="verificationEnabled" aria-label="注册时验证邮箱" />
          </div>
          <PageAlert v-if="verificationEnabled && !smtpReady" tone="warning" title="SMTP 通道尚未就绪">请先在“邮件与运营模板”完成 SMTP 主机、发件人、TLS 和凭证配置，再启用邮箱验证。</PageAlert>
          <div class="section-actions"><RouterLink class="settings-link" to="/admin/settings/email">配置邮件通道</RouterLink><UiButton type="button" :loading="savingVerification" :disabled="verificationRecoveryConflict || !verificationDirty || (verificationEnabled && !smtpReady)" @click="saveVerification">保存邮箱验证</UiButton></div>
        </UiSection>


      </div>

      <UiSection title="注册成功通知" description="用户注册成功后自动发送，停用不影响验证码。">
        <EmailTemplateManager mode="registration" @dirty="templateDirty = $event" />
      </UiSection>
    </SettingsPageLayout>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchSystemConfigs, updateSiteSettings, updateSystemConfig, type SystemConfig } from '../api/client'
import EmailTemplateManager from '../components/EmailTemplateManager.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import SettingsPageLayout from '../components/SettingsPageLayout.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiButton from '../components/UiButton.vue'
import UiSwitch from '../components/UiSwitch.vue'
import UiSection from '../components/UiSection.vue'
import { useUnsavedChangesGuard } from '../composables/useFormState'
import { useReauthenticationDraft } from '../composables/useReauthenticationDraft'
import { registrationRecoveryDraft, type SiteBaseline } from '../utils/settingsReauthenticationDraftSchemas'
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
const recoveryReady = ref(false)
const registrationRecoveryConflict = ref(false)
const verificationRecoveryConflict = ref(false)
const registrationBaseline = ref<SiteBaseline | null>(null)
const verificationOrigin = ref<{ baseline: boolean; revision: number } | null>(null)
const registrationDraftRecovery = useReauthenticationDraft({
  scope: 'admin.settings.registration', permission: 'admin', ready: () => recoveryReady.value,
  read: () => registrationDirty.value || verificationDirty.value || verificationRecoveryConflict.value ? {
    registration: registrationDirty.value ? { value: registrationEnabled.value, baseline: registrationBaseline.value } : null,
    verification: verificationDirty.value || verificationRecoveryConflict.value ? { value: verificationEnabled.value, ...(verificationOrigin.value || { baseline: initialVerificationEnabled.value, revision: registrationConfig.value?.revision }) } : null,
  } : null,
  sanitize: registrationRecoveryDraft,
  restore: value => {
    if (value.registration) {
      registrationRecoveryConflict.value = JSON.stringify(value.registration.baseline) !== JSON.stringify(registrationBaseline.value)
      registrationEnabled.value = value.registration.value; registrationBaseline.value = value.registration.baseline
    }
    if (value.verification) {
      verificationRecoveryConflict.value = !registrationConfig.value || registrationConfig.value.revision !== value.verification.revision || initialVerificationEnabled.value !== value.verification.baseline
      verificationOrigin.value = { baseline: value.verification.baseline, revision: value.verification.revision }
      verificationEnabled.value = value.verification.value
    }
  },
})

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
  recoveryReady.value = false
  loading.value = true; error.value = ''
  try {
    const [setup, nextConfigs] = await Promise.all([app.loadSetupStatus(true), fetchSystemConfigs()])
    configs.value = nextConfigs
    registrationEnabled.value = Boolean(setup?.allow_registration)
    initialRegistrationEnabled.value = registrationEnabled.value
    const verification = nextConfigs.find(item => item.config_key === 'register_email_verification')
    verificationEnabled.value = verification?.value === true
    initialVerificationEnabled.value = verificationEnabled.value
    if (!setup) throw new Error('站点安装信息尚未加载。')
    registrationBaseline.value = { site_name: setup.site_name || app.siteName || 'zboard', site_url: setup.site_url || window.location.origin, allow_registration: Boolean(setup.allow_registration) }
    registrationRecoveryConflict.value = false; verificationRecoveryConflict.value = false; verificationOrigin.value = null
    recoveryReady.value = true
  } catch (cause: any) { error.value = normalizeApiErrorMessage(cause, '注册设置加载失败。') }
  finally { loading.value = false }
}

async function reload() {
  if ((registrationDirty.value || verificationDirty.value) && !await confirmAction({ title: '重新载入注册设置？', message: '未保存的开关修改将被服务器当前值替换。', confirmText: '重新载入', tone: 'danger' })) return
  await load()
}

async function saveRegistration() {
  if (registrationRecoveryConflict.value) return
  const setup = app.installation
  if (!setup) { error.value = '站点安装信息尚未加载。'; return }
  savingRegistration.value = true; message.value = ''; error.value = ''
  try {
    await updateSiteSettings({ site_name: setup.site_name || app.siteName || 'zboard', site_url: setup.site_url || window.location.origin, allow_registration: registrationEnabled.value })
    await app.loadSetupStatus(true)
    initialRegistrationEnabled.value = registrationEnabled.value
    registrationBaseline.value = { site_name: setup.site_name || app.siteName || 'zboard', site_url: setup.site_url || window.location.origin, allow_registration: registrationEnabled.value }
    message.value = registrationEnabled.value ? '公开注册入口已开放。' : '公开注册入口已关闭。'
  } catch (cause: any) { error.value = normalizeApiErrorMessage(cause, '注册入口保存失败。') }
  finally { savingRegistration.value = false }
}

async function saveVerification() {
  if (verificationRecoveryConflict.value) return
  const config = registrationConfig.value
  if (!config) { error.value = '系统未返回注册邮箱验证码配置。'; return }
  savingVerification.value = true; message.value = ''; error.value = ''
  try {
    const updated = await updateSystemConfig(config.config_key, verificationEnabled.value, config.revision)
    const index = configs.value.findIndex(item => item.config_key === updated.config_key)
    if (index >= 0) configs.value[index] = updated
    initialVerificationEnabled.value = verificationEnabled.value
    verificationOrigin.value = null
    await app.loadPublicConfigs()
    message.value = verificationEnabled.value ? '注册邮箱验证码已启用。' : '注册邮箱验证码已停用。'
  } catch (cause: any) { error.value = normalizeApiErrorMessage(cause, '邮箱验证设置保存失败。') }
  finally { savingVerification.value = false }
}

onMounted(load)
</script>

<style scoped>
.registration-grid { display: grid; gap: 28px; }
.registration-settings-page :deep(.settings-page-content .ui-section > .panel-header) { min-height: 0; padding: 0 0 8px; border: 0; }
.registration-settings-page :deep(.settings-page-content .ui-section > .panel-body) { padding: 0; }
.setting-control { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 10px 0; }
.setting-control strong { font-size: 13px; }
.setting-control p { margin: 4px 0 0; color: var(--muted); font-size: 12px; line-height: 1.55; }
.section-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 10px; padding-top: 8px; }
.settings-link { margin-right: auto; color: var(--primary); font-size: 12px; font-weight: 600; text-decoration: none; }
</style>
