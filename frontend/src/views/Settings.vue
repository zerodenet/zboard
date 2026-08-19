<template>
  <section class="standard-page">
    <PageHeader title="系统设置" description="管理站点公开信息和运行配置。部署密钥仍由服务器环境负责，不在页面中暴露。" eyebrow="Configuration">
      <template #actions><PageRefreshButton label="刷新系统设置" :loading="loading" @click="reloadSettings" /></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="系统设置已保存" error-title="设置操作失败" />

    <div class="section-grid settings-top">
      <UiSection class="span-7" title="基础站点信息" description="公开名称、访问地址和注册策略。">
        <template #meta><StatusBadge :tone="siteState.dirty.value ? 'warning' : 'info'" :icon="siteState.dirty.value ? 'edit' : 'info'">{{ siteState.dirty.value ? '有未保存修改' : '公开配置' }}</StatusBadge></template>
        <form ref="siteFormElement" class="panel-body stack" novalidate @submit.prevent="saveSiteSettings">
          <div class="form-grid">
            <FormField v-slot="{ controlAttrs }" label="站点名称" name="settings-site-name" :error="siteErrors.fields.site_name" required><UiInput v-model.trim="form.site_name" v-bind="controlAttrs" maxlength="80" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="公开访问地址" name="settings-site-url" hint="用于订阅链接和外部页面跳转。" :error="siteErrors.fields.site_url" required><UiInput v-model.trim="form.site_url" v-bind="controlAttrs" type="url" /></FormField>
          </div>
          <label class="registration-switch"><span class="switch-copy"><strong>开放用户注册</strong><small>允许访客通过登录页创建普通账户。</small></span><UiCheckbox v-model="form.allow_registration" role="switch" /></label>
          <PageAlert v-if="siteErrors.formError.value" tone="danger" title="站点设置未保存">{{ siteErrors.formError.value }}</PageAlert>
          <div class="form-actions"><UiButton type="submit" :loading="savingSite" :disabled="!siteState.dirty.value">保存站点设置</UiButton></div>
        </form>
      </UiSection>

      <UiSection class="span-5" title="安全边界" description="下列敏感项不属于浏览器配置。">
        <template #meta><UiIcon class="security-header" name="shield" /></template>
        <div class="panel-body security-list">
          <div><span><UiIcon name="database" /></span><div><strong>数据库凭证</strong><p>由部署环境变量管理</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
          <div><span><UiIcon name="key" /></span><div><strong>JWT 签名密钥</strong><p>服务启动时校验强度</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
          <div><span><UiIcon name="shield" /></span><div><strong>凭证加密密钥</strong><p>用于节点与协议敏感配置</p></div><StatusBadge tone="success">受保护</StatusBadge></div>
        </div>
      </UiSection>
    </div>

    <UiSection title="动态配置" description="修改后立即写入数据库；公开站点配置会同步刷新当前页面。密钥类配置不会回显原值。">
      <template #meta><span class="config-count">{{ operationalConfigs.length }} 项</span></template>
      <div v-if="operationalConfigs.length" class="config-groups">
        <section v-if="siteCustomizationConfigs.length" class="config-section">
          <header><span class="config-section-icon"><UiIcon name="info" /></span><div><h3>站点品牌与公开内容</h3><p>配置 Logo、描述、首页文案、页脚、联系方式、法律/注册信息和 SEO；可选项留空时使用默认展示。</p></div></header>
          <div class="config-list"><ConfigRow v-for="config in siteCustomizationConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :dirty="configDirty(config)" :saving="savingKey === config.config_key" :error="configErrors[config.config_key]" :conflict="Boolean(configConflicts[config.config_key])" @update:draft="updateDraft(config.config_key, $event)" @reload="reloadConfig(config.config_key)" @save="saveConfig(config)" /></div>
        </section>
        <section v-if="emailConfigs.length" class="config-section">
          <header><span class="config-section-icon"><UiIcon name="audit" /></span><div><h3>邮件与通知</h3><p>先完成 SMTP 配置，再启用邮件任务。</p></div></header>
          <div class="config-list"><ConfigRow v-for="config in emailConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :dirty="configDirty(config)" :saving="savingKey === config.config_key" :error="configErrors[config.config_key]" :conflict="Boolean(configConflicts[config.config_key])" @update:draft="updateDraft(config.config_key, $event)" @reload="reloadConfig(config.config_key)" @save="saveConfig(config)" /></div>
        </section>
        <section v-if="otherConfigs.length" class="config-section">
          <header><span class="config-section-icon"><UiIcon name="settings" /></span><div><h3>其他运行参数</h3><p>系统时区、保留策略和其他运行行为配置。</p></div></header>
          <div class="config-list"><ConfigRow v-for="config in otherConfigs" :key="config.config_key" :config="config" :draft="drafts[config.config_key]" :dirty="configDirty(config)" :saving="savingKey === config.config_key" :error="configErrors[config.config_key]" :conflict="Boolean(configConflicts[config.config_key])" @update:draft="updateDraft(config.config_key, $event)" @reload="reloadConfig(config.config_key)" @save="saveConfig(config)" /></div>
        </section>
      </div>
      <EmptyState v-else icon="settings" title="没有动态配置" description="系统当前未返回可在页面中修改的运行配置。" />
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { fetchSystemConfigs, updateSiteSettings, updateSystemConfig, type SystemConfig } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import ConfigRow from '../components/SettingsConfigRow.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useAppStore } from '../stores/app'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { confirmAction } from '../utils/feedback'
import { formatSystemConfigDraft, normalizeSystemConfigDraft } from '../utils/systemConfig'
import { collectFieldErrors, isHttpUrl, isUtf8LengthInRange } from '../utils/validation'

const app = useAppStore()
const loading = ref(false)
const savingSite = ref(false)
const savingKey = ref('')
const message = ref('')
const error = ref('')
const configs = ref<SystemConfig[]>([])
const drafts = reactive<Record<string, unknown>>({})
const configErrors = reactive<Record<string, string>>({})
const configConflicts = reactive<Record<string, boolean>>({})
const form = reactive({ site_name: '', site_url: '', allow_registration: true })
const siteFormElement = ref<HTMLElement | null>(null)
const siteErrors = useFormErrors()
const siteState = useDirtyForm(() => form)
const hiddenSiteKeys = new Set(['site_name', 'site_url', 'register_switch'])
const siteCustomizationKeys = new Set([
  'site_desc', 'site_logo', 'site_logo_dark', 'site_favicon', 'site_footer_copyright',
  'site_support_email', 'site_support_url', 'site_telegram_url', 'site_terms_url',
  'site_privacy_url', 'site_refund_url', 'site_legal_items', 'site_meta_title',
  'site_meta_description', 'site_home_kicker', 'site_home_title', 'site_home_primary_cta',
])
const operationalConfigs = computed(() => configs.value.filter(item => !hiddenSiteKeys.has(item.config_key)).sort((a, b) => a.id - b.id))
const siteCustomizationConfigs = computed(() => operationalConfigs.value.filter(item => siteCustomizationKeys.has(item.config_key)))
const emailConfigs = computed(() => operationalConfigs.value.filter(item => !siteCustomizationKeys.has(item.config_key) && /smtp|email/i.test(item.config_key)))
const otherConfigs = computed(() => operationalConfigs.value.filter(item => !siteCustomizationKeys.has(item.config_key) && !/smtp|email/i.test(item.config_key)))
const configChanges = computed(() => operationalConfigs.value.filter(configDirty).length)
const pageDirty = computed(() => siteState.dirty.value || configChanges.value > 0)
useUnsavedChangesGuard(
  () => pageDirty.value,
  () => confirmAction({
    title: '离开系统设置？',
    message: `当前有 ${configChanges.value + (siteState.dirty.value ? 1 : 0)} 组修改尚未保存。`,
    confirmText: '放弃修改',
    tone: 'danger',
  }),
)

watch(() => form.site_name, () => siteErrors.clear('site_name'))
watch(() => form.site_url, () => siteErrors.clear('site_url'))

function configValue(config: SystemConfig) {
  return formatSystemConfigDraft(config)
}

function sameValue(left: unknown, right: unknown) {
  return JSON.stringify(left ?? null) === JSON.stringify(right ?? null)
}

function configDirty(config: SystemConfig) {
  return !sameValue(drafts[config.config_key], configValue(config))
}

function updateDraft(key: string, value: unknown) {
  drafts[key] = value
  if (!configConflicts[key]) delete configErrors[key]
}

async function loadConfigs() {
  const next = await fetchSystemConfigs()
  configs.value = next
  for (const key of Object.keys(drafts)) delete drafts[key]
  for (const key of Object.keys(configErrors)) delete configErrors[key]
  for (const key of Object.keys(configConflicts)) delete configConflicts[key]
  for (const config of next) drafts[config.config_key] = configValue(config)
}

async function loadAllData() {
  loading.value = true; error.value = ''
  try {
    const [settings] = await Promise.all([app.loadSetupStatus(true), loadConfigs()])
    form.site_name = settings?.site_name || 'zboard'
    form.site_url = settings?.site_url || window.location.origin
    form.allow_registration = settings?.allow_registration ?? true
    siteErrors.clear()
    siteState.markClean()
  }
  catch (e: any) { error.value = e?.response?.data?.message || '系统设置加载失败。' }
  finally { loading.value = false }
}

async function reloadSettings() {
  if (pageDirty.value && !await confirmAction({
    title: '重新载入并放弃修改？',
    message: `当前有 ${configChanges.value + (siteState.dirty.value ? 1 : 0)} 组未保存修改。重新载入将使用服务器当前值。`,
    confirmText: '重新载入',
    tone: 'danger',
  })) return
  await loadAllData()
}

async function saveSiteSettings() {
  form.site_name = form.site_name.trim()
  form.site_url = form.site_url.trim().replace(/\/+$/, '')
  const valid = await siteErrors.applyValidation(collectFieldErrors({
    site_name: !isUtf8LengthInRange(form.site_name, 1, 80, true) && '站点名称必须为 1–80 个 UTF-8 字节。',
    site_url: !isHttpUrl(form.site_url) && '请输入不含账号、密码或片段的完整 HTTP 或 HTTPS 地址。',
  }), siteFormElement, '请更正标记字段后再保存站点设置。')
  if (!valid) return
  savingSite.value = true; message.value = ''
  try {
    await updateSiteSettings(form)
    await app.loadSetupStatus(true)
    siteState.markClean()
    message.value = '站点设置已保存。'
  } catch (e: any) {
    await siteErrors.applyApiError(e, '站点设置保存失败。', siteFormElement, { site_name: 'site_name', site_url: 'site_url' })
  } finally { savingSite.value = false }
}

async function saveConfig(config: SystemConfig) {
  const key = config.config_key
  if (!configDirty(config)) return
  message.value = ''; delete configErrors[key]
  const normalized = normalizeSystemConfigDraft(config, drafts[key])
  if (normalized.error) {
    configErrors[key] = normalized.error
    await nextTick()
    document.getElementById(`${key}-control`)?.focus()
    return
  }
  savingKey.value = key
  try {
    const updated = await updateSystemConfig(key, normalized.value, config.revision)
    const index = configs.value.findIndex(item => item.config_key === key)
    if (index >= 0) configs.value[index] = updated
    drafts[key] = configValue(updated)
    delete configConflicts[key]
    if (siteCustomizationKeys.has(key)) await app.loadPublicConfigs()
    message.value = `${config.name} 已保存。`
  } catch (e: any) {
    if (e?.response?.status === 409) {
      configConflicts[key] = true
      configErrors[key] = '服务器版本已变化。请重新载入当前值，确认后再保存。'
    } else {
      configErrors[key] = normalizeApiErrorMessage(e, '运行配置保存失败。')
    }
  } finally { savingKey.value = '' }
}

async function reloadConfig(key: string) {
  savingKey.value = key
  try {
    const latest = (await fetchSystemConfigs()).find(item => item.config_key === key)
    if (!latest) throw new Error('服务器未返回该配置。')
    const index = configs.value.findIndex(item => item.config_key === key)
    if (index >= 0) configs.value[index] = latest
    drafts[key] = configValue(latest)
    delete configErrors[key]
    delete configConflicts[key]
    message.value = `${latest.name} 已重新载入。`
  } catch (e: any) {
    configErrors[key] = normalizeApiErrorMessage(e, '重新载入失败。')
  } finally { savingKey.value = '' }
}

onMounted(loadAllData)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.settings-top { margin-bottom: 16px; }.registration-switch { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-soft); }.switch-copy { display: grid; gap: 3px; }.switch-copy strong { font-size: 13px; }.switch-copy small { color: var(--muted); font-size: 11px; }.security-header { color: var(--success); font-size: 20px; }.security-list { display: grid; gap: 4px; }.security-list > div { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 11px; padding: 10px 0; }.security-list > div + div { border-top: 1px solid var(--line); }.security-list > div > span:first-child { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 9px; color: var(--success); background: var(--success-soft); }.security-list strong { font-size: 12px; }.security-list p { margin: 2px 0 0; color: var(--muted); font-size: 10px; }.config-count { color: var(--muted); font-size: 12px; }.config-groups { display: grid; }.config-section { padding: 20px; }.config-section + .config-section { border-top: 1px solid var(--line); }.config-section > header { display: flex; gap: 10px; margin-bottom: 14px; }.config-section-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 9px; color: var(--primary); background: var(--primary-soft); }.config-section h3 { margin: 1px 0 2px; font-size: 14px; }.config-section header p { margin: 0; color: var(--muted); font-size: 10px; }.config-list { display: grid; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
:deep(.config-row) { display: grid; grid-template-columns: minmax(260px, 1.2fr) minmax(220px, .8fr) auto; align-items: center; gap: 16px; padding: 14px; background: var(--surface); }:deep(.config-row + .config-row) { border-top: 1px solid var(--line); }:deep(.config-copy) { min-width: 0; display: grid; gap: 4px; }:deep(.config-copy > div) { display: flex; align-items: center; gap: 7px; }:deep(.config-copy strong) { font-size: 12px; }:deep(.value-type) { padding: 2px 5px; border-radius: 4px; color: var(--primary); background: var(--primary-soft); font-size: 9px; font-weight: 700; }:deep(.config-copy code) { color: var(--code-text); font-size: 10px; }:deep(.config-copy p) { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.45; }:deep(.config-input) { min-height: 36px !important; }:deep(.config-switch) { display: flex; align-items: center; justify-content: flex-end; gap: 8px; color: var(--muted); font-size: 11px; }
@media (max-width: 850px) { :deep(.config-row) { grid-template-columns: 1fr; align-items: stretch; }:deep(.config-switch) { justify-content: flex-start; } }
</style>